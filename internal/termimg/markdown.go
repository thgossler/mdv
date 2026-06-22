package termimg

import (
	"errors"
	"strings"
	"sync"
)

// Renderer turns markdown image sources into terminal cell blocks for a fixed
// protocol. It satisfies the mdfmt.ImageRenderer interface. Decoded images are
// cached by source so repeated or co-located images (e.g. badge rows) are only
// fetched and decoded once.
type Renderer struct {
	protocol    Protocol
	allowRemote bool

	mu        sync.Mutex
	baseDir   string
	deferLoad bool
	cache     map[string]cacheEntry
}

// errDeferred is returned by decode for an uncached image while deferLoad is
// set, so the render keeps the alt text instead of blocking on a file read,
// SVG rasterization, or network fetch. A background Prefetch loads the image
// and a later re-render then draws it from the cache. Keeping all image loading
// off the render path means an external scanner (e.g. Defender) cannot stall the
// UI when it intercepts those reads.
var errDeferred = errors.New("termimg: image load deferred")

func isRemoteSrc(src string) bool {
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

type cacheEntry struct {
	dec Decoded
	err error
}

// displayScale shrinks every image relative to its GUI size so pictures do not
// dominate the narrower terminal column.
const displayScale = 0.62

// refDocWidthPx is the reference document width (matching the GUI default
// contentWidthPx) used to size images relative to the column, mirroring the GUI
// where a small badge stays small and only large images fill the width.
const refDocWidthPx = 860.0

// NewRenderer builds a Renderer for the given protocol. A ProtocolNone renderer
// renders nothing (callers keep alt text).
func NewRenderer(p Protocol, baseDir string, allowRemote bool) *Renderer {
	return &Renderer{
		protocol:    p,
		baseDir:     baseDir,
		allowRemote: allowRemote,
		cache:       make(map[string]cacheEntry),
	}
}

// Enabled reports whether the renderer will attempt to draw images.
func (r *Renderer) Enabled() bool {
	return r != nil && r.protocol != ProtocolNone
}

// SetBaseDir updates the base directory used to resolve relative image paths.
// The decode cache is retained: remote images stay cached across documents and
// file images remain namespaced by base directory.
func (r *Renderer) SetBaseDir(dir string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.baseDir = dir
	r.mu.Unlock()
}

// SetDeferLoad controls whether the synchronous render path skips uncached
// images (keeping their alt text) instead of loading them inline. In-memory
// data URIs are always loaded, since they involve no file or network IO. The
// TUI sets this so opening or re-rendering a document never blocks on image IO;
// a background Prefetch loads the images and a re-render then draws them.
func (r *Renderer) SetDeferLoad(v bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.deferLoad = v
	r.mu.Unlock()
}

// SetAllowRemote toggles fetching of http(s) images at runtime. When remote
// loading is turned on, cached remote entries (which may hold a "blocked"
// failure from while it was off) are dropped so a follow-up Prefetch/render can
// load them. The TUI uses this for its session-only remote-image toggle.
func (r *Renderer) SetAllowRemote(v bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.allowRemote == v {
		return
	}
	r.allowRemote = v
	for key := range r.cache {
		if isRemoteSrc(key) {
			delete(r.cache, key)
		}
	}
}

// AllowRemote reports whether remote (http(s)) image fetching is currently
// enabled.
func (r *Renderer) AllowRemote() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.allowRemote
}

// cacheKey returns the cache key for src. Remote URLs and data URIs are
// self-identifying and shared across documents; file paths are namespaced by
// the current base directory so the same relative name in different folders
// does not collide.
func (r *Renderer) cacheKey(src string) string {
	if isRemoteSrc(src) || strings.HasPrefix(src, "data:") {
		return src
	}
	r.mu.Lock()
	b := r.baseDir
	r.mu.Unlock()
	return b + "\x00" + src
}

// decode loads and decodes src, caching the result (including failures) so each
// source is only fetched once. When deferLoad is set, an uncached image (other
// than an in-memory data URI) is reported as a miss without loading, so the
// render never blocks on file or network IO.
func (r *Renderer) decode(src string) (Decoded, error) {
	key := r.cacheKey(src)

	r.mu.Lock()
	if e, found := r.cache[key]; found {
		r.mu.Unlock()
		return e.dec, e.err
	}
	deferLoad := r.deferLoad
	baseDir := r.baseDir
	allowRemote := r.allowRemote
	r.mu.Unlock()

	if deferLoad && !strings.HasPrefix(src, "data:") {
		return Decoded{}, errDeferred
	}

	dec, err := LoadDecoded(src, LoadOptions{BaseDir: baseDir, AllowRemote: allowRemote})

	r.mu.Lock()
	r.cache[key] = cacheEntry{dec: dec, err: err}
	r.mu.Unlock()
	return dec, err
}

// renderResult renders a decoded image to a cell block (uncentered), sized
// relative to the GUI for a width-column document. dispW, when > 0, overrides
// the image's intrinsic display width (e.g. an author-specified <img width>).
func (r *Renderer) renderResult(dec Decoded, width, dispW int) (RenderResult, bool) {
	// Leave a small horizontal margin so the picture never collides with the
	// document's right edge.
	budget := width - 2
	if budget < 1 {
		budget = width
	}
	// Size the image relative to a reference document width and shrink slightly,
	// never upscaling a small image to the full width. An explicit display width
	// (from markup) takes precedence over the intrinsic pixel size.
	displayW := dec.DisplayW
	if dispW > 0 {
		displayW = dispW
	}
	if displayW > 0 {
		intrinsicCols := int(float64(width)*float64(displayW)/refDocWidthPx*displayScale + 0.5)
		if intrinsicCols < 1 {
			intrinsicCols = 1
		}
		if intrinsicCols < budget {
			budget = intrinsicCols
		}
	}
	res, err := Render(dec.Image, r.protocol, budget)
	if err != nil || res.Text == "" {
		return RenderResult{}, false
	}
	return res, true
}

// RenderImage decodes the image at src and returns a terminal cell block sized
// to fit within colWidth columns and horizontally centered. dispW, when > 0, is
// the author-specified display width in CSS pixels. It returns ok=false when the
// image cannot be rendered, so the caller keeps the alt text.
func (r *Renderer) RenderImage(src, alt string, colWidth, dispW int) (string, bool) {
	if !r.Enabled() || colWidth < 2 {
		return "", false
	}
	dec, err := r.decode(src)
	if err != nil {
		return "", false
	}
	res, ok := r.renderResult(dec, colWidth, dispW)
	if !ok {
		return "", false
	}
	return center(res, colWidth), true
}

// RenderImageRow renders several images side by side, wrapping onto further
// visual rows when they exceed the column, mirroring how inline images flow in
// the GUI. Images that cannot be rendered are skipped; ok is false only when
// none could be rendered (so the caller keeps the alt text).
func (r *Renderer) RenderImageRow(srcs []string, width int) (string, bool) {
	if !r.Enabled() || width < 2 || len(srcs) == 0 {
		return "", false
	}
	var imgs []RenderResult
	for _, s := range srcs {
		dec, err := r.decode(s)
		if err != nil {
			continue
		}
		if res, ok := r.renderResult(dec, width, 0); ok {
			imgs = append(imgs, res)
		}
	}
	switch len(imgs) {
	case 0:
		return "", false
	case 1:
		return center(imgs[0], width), true
	default:
		return composeRow(imgs, width), true
	}
}

// PrewarmImages decodes the given sources concurrently, filling the cache so
// subsequent render calls return immediately. This keeps startup responsive
// when a document fetches many remote images (e.g. badge rows). Failures are
// cached as such so the caller later falls back to alt text.
func (r *Renderer) PrewarmImages(srcs []string, width int) {
	if !r.Enabled() {
		return
	}
	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	seen := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true

		key := r.cacheKey(s)
		r.mu.Lock()
		_, done := r.cache[key]
		r.mu.Unlock()
		if done {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(src string) {
			defer wg.Done()
			defer func() { <-sem }()
			r.decode(src)
		}(s)
	}
	wg.Wait()
}

// Prefetch loads and caches the given image sources, fetching remote images
// even when deferRemote is set. It is meant to run off the render path (e.g. in
// a Bubble Tea command) so a later re-render draws the images from cache without
// the render ever blocking on the network. Already-cached sources are skipped.
func (r *Renderer) Prefetch(srcs []string) {
	if !r.Enabled() {
		return
	}
	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	seen := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true

		key := r.cacheKey(s)
		r.mu.Lock()
		_, done := r.cache[key]
		baseDir := r.baseDir
		allowRemote := r.allowRemote
		r.mu.Unlock()
		if done {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(src, key, baseDir string, allowRemote bool) {
			defer wg.Done()
			defer func() { <-sem }()
			dec, err := LoadDecoded(src, LoadOptions{BaseDir: baseDir, AllowRemote: allowRemote})
			r.mu.Lock()
			r.cache[key] = cacheEntry{dec: dec, err: err}
			r.mu.Unlock()
		}(s, key, baseDir, allowRemote)
	}
	wg.Wait()
}

// center horizontally pads a render result within width columns. For half-block
// output every line is padded; for pixel protocols only the leading line is
// padded so escape sequences are not split.
func center(res RenderResult, width int) string {
	pad := (width - res.Cols) / 2
	if pad <= 0 {
		return res.Text
	}
	prefix := strings.Repeat(" ", pad)

	if !strings.Contains(res.Text, "\n") {
		return prefix + res.Text
	}
	// Distinguish multi-line half-blocks (every line is a row of cells) from
	// pixel protocols whose internal newlines are cursor movement only.
	if isBlocks(res) {
		lines := strings.Split(res.Text, "\n")
		for i, ln := range lines {
			lines[i] = prefix + ln
		}
		return strings.Join(lines, "\n")
	}
	return prefix + res.Text
}

// isBlocks reports whether res came from the half-block renderer by checking
// that its row count matches the number of text lines.
func isBlocks(res RenderResult) bool {
	return res.Rows > 1 && strings.Count(res.Text, "\n") == res.Rows-1
}
