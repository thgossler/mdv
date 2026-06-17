package mdfmt

import (
	"regexp"
	"strings"
)

// ImageRenderer renders a markdown image into a block of terminal cells. It is
// implemented by internal/termimg; mdfmt depends only on this interface so the
// two packages stay decoupled.
type ImageRenderer interface {
	// RenderImage returns the terminal cell block for the image at src, sized to
	// fit colWidth columns and centered. displayWidthPx is the author-specified
	// display width in CSS pixels (e.g. an <img width> attribute), or 0 to use
	// the image's intrinsic size. ok is false when the image cannot be rendered,
	// in which case the caller keeps the alt text.
	RenderImage(src, alt string, colWidth, displayWidthPx int) (block string, ok bool)
}

// imagePrewarmer is an optional capability: a renderer that can decode several
// images concurrently up front so subsequent RenderImage calls are cache hits.
type imagePrewarmer interface {
	PrewarmImages(srcs []string, width int)
}

// imageRowRenderer is an optional capability: a renderer that can lay several
// images out side by side (wrapping as needed), like inline images in the GUI.
type imageRowRenderer interface {
	RenderImageRow(srcs []string, width int) (block string, ok bool)
}

const (
	imgTokenPrefix = "\uF8FFmdvIMG"
	imgTokenSuffix = "\uF8FF"
)

// imgCore matches a markdown image, optionally wrapped in a link, without line
// anchors: ![alt](src) or [![alt](src)](href).
const imgCore = `\[?!\[[^\]]*\]\([^)\s]+(?:\s+[^)]*)?\)(?:\]\([^)]+\))?`

var (
	// Standalone markdown image on its own line, optionally wrapped in a link:
	//   ![alt](src)            or   [![alt](src)](href)
	reStandaloneImg = regexp.MustCompile(`(?m)^[ \t]*\[?!\[([^\]]*)\]\(([^)\s]+)(?:\s+[^)]*)?\)(?:\]\([^)]+\))?[ \t]*$`)

	// A run of one or more consecutive standalone-image lines (a "badge row").
	// Such images are laid out side by side, matching the GUI where inline
	// images flow in the same row and wrap.
	reImageRun = regexp.MustCompile(`(?m)^[ \t]*` + imgCore + `[ \t]*(?:\r?\n[ \t]*` + imgCore + `[ \t]*)*$`)

	// HTML <img> tag (block or inline). Captured whole; src/alt pulled out after.
	reHTMLImg    = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reSrcAttr    = regexp.MustCompile(`(?is)\bsrc\s*=\s*["']([^"']*)["']`)
	reAltAttr    = regexp.MustCompile(`(?is)\balt\s*=\s*["']([^"']*)["']`)
	reWidthAttr  = regexp.MustCompile(`(?is)\bwidth\s*=\s*["']?\s*(\d+)`)
	reHeightAttr = regexp.MustCompile(`(?is)\bheight\s*=\s*["']?\s*(\d+)`)

	// Azure DevOps / extended markdown image size hint: ![alt](src =800x600),
	// ![alt](src =800x) or ![alt](src =x600). Width is captured when present.
	reMdImgSize = regexp.MustCompile(`=\s*(\d+)?x?(\d+)?\s*\)`)

	// Matches a rendered line that carries an image token, for post-render
	// substitution. The whole line is replaced regardless of surrounding ANSI.
	reImgTokenLine = regexp.MustCompile(`(?m)^.*\x{F8FF}mdvIMG(\d+)\x{F8FF}.*$`)
)

// extractImages replaces renderable images in src with opaque tokens and
// returns the rewritten source plus a token->block substitution table. Images
// that cannot be rendered are left untouched so their alt text is shown.
func extractImages(src string, r ImageRenderer, width int) (string, map[string]string) {
	subs := make(map[string]string)
	next := 0

	// Decode all referenced images concurrently first, so a document with many
	// remote images (badge rows) does not stall while they are fetched one by
	// one below.
	if pw, ok := r.(imagePrewarmer); ok {
		pw.PrewarmImages(collectImageSrcs(src), width)
	}

	token := func(block string) string {
		t := imgTokenPrefix + itoa(next) + imgTokenSuffix
		next++
		subs[t] = block
		return t
	}

	// HTML <img> first so that surrounding <a>/<div> wrappers are still present
	// to be stripped by convertHTML later. An explicit width attribute (or, if
	// absent, a height attribute combined with the image's own aspect) sizes the
	// picture, matching the GUI.
	src = reHTMLImg.ReplaceAllStringFunc(src, func(m string) string {
		srcAttr := firstGroup(reSrcAttr, m)
		if srcAttr == "" {
			return m
		}
		alt := firstGroup(reAltAttr, m)
		dispW := firstInt(reWidthAttr, m)
		if block, ok := r.RenderImage(srcAttr, alt, width, dispW); ok {
			return token(block)
		}
		return m
	})

	// Standalone markdown images. Consecutive image-only lines (a badge row)
	// are laid out side by side; a lone image is rendered on its own.
	src = reImageRun.ReplaceAllStringFunc(src, func(run string) string {
		matches := reStandaloneImg.FindAllStringSubmatch(run, -1)
		if len(matches) == 0 {
			return run
		}
		if len(matches) == 1 {
			alt, imgSrc := matches[0][1], matches[0][2]
			dispW := mdImgWidth(run)
			if block, ok := r.RenderImage(imgSrc, alt, width, dispW); ok {
				// Isolate as its own paragraph so glamour renders it standalone.
				return "\n\n" + token(block) + "\n\n"
			}
			return run
		}
		srcs := make([]string, len(matches))
		for i, m := range matches {
			srcs[i] = m[2]
		}
		if rr, ok := r.(imageRowRenderer); ok {
			if block, ok2 := rr.RenderImageRow(srcs, width); ok2 {
				return "\n\n" + token(block) + "\n\n"
			}
		}
		return run
	})

	return src, subs
}

// substituteImages replaces token-carrying lines in rendered output with their
// image blocks.
func substituteImages(rendered string, subs map[string]string) string {
	if len(subs) == 0 {
		return rendered
	}
	return reImgTokenLine.ReplaceAllStringFunc(rendered, func(line string) string {
		m := reImgTokenLine.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		token := imgTokenPrefix + m[1] + imgTokenSuffix
		if block, ok := subs[token]; ok {
			return block
		}
		return line
	})
}

// collectImageSrcs returns the source references of all renderable images in
// src (HTML <img> tags and standalone markdown images), for concurrent
// prewarming. Code spans are already protected by the caller, so none are
// matched here.
func collectImageSrcs(src string) []string {
	var out []string
	for _, m := range reHTMLImg.FindAllString(src, -1) {
		if s := firstGroup(reSrcAttr, m); s != "" {
			out = append(out, s)
		}
	}
	for _, m := range reStandaloneImg.FindAllStringSubmatch(src, -1) {
		if len(m) > 2 && m[2] != "" {
			out = append(out, m[2])
		}
	}
	return out
}

func firstGroup(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// firstInt returns the first capture group of re in s parsed as a non-negative
// integer, or 0 when absent or unparseable.
func firstInt(re *regexp.Regexp, s string) int {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n := 0
	for _, c := range strings.TrimSpace(m[1]) {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// mdImgWidth extracts the width from an extended markdown image size hint
// (![alt](src =800x600)), or 0 when none is present.
func mdImgWidth(s string) int {
	m := reMdImgSize.FindStringSubmatch(s)
	if m == nil || m[1] == "" {
		return 0
	}
	n := 0
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
