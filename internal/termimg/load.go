package termimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Register decoders for the standard raster formats.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // WebP decoder registration
)

// maxImageBytes caps how many bytes are read for a single image, protecting
// against pathological or hostile sources. It is generous enough for typical
// documentation imagery.
const maxImageBytes = 25 << 20 // 25 MiB

// remoteTimeout bounds how long a single remote image fetch may take so that a
// slow host cannot stall rendering.
const remoteTimeout = 8 * time.Second

// LoadOptions controls how an image reference is resolved.
type LoadOptions struct {
	// BaseDir is the directory used to resolve relative file references.
	BaseDir string
	// AllowRemote permits fetching http(s) URLs. When disabled, remote
	// references resolve to an error so the caller can fall back to alt text.
	AllowRemote bool
}

// Decoded is a decoded image plus its intrinsic display size in CSS pixels
// (the SVG viewBox for vector sources, or the pixel bounds for raster images).
// The display size is used to scale the image to a GUI-like relative size in
// the terminal rather than always filling the content column.
type Decoded struct {
	Image    image.Image
	DisplayW int
	DisplayH int
}

// Load resolves a markdown image source (a file path, data: URI, or http(s)
// URL) and decodes it into an image. SVG sources are rasterized.
func Load(src string, opt LoadOptions) (image.Image, error) {
	d, err := LoadDecoded(src, opt)
	if err != nil {
		return nil, err
	}
	return d.Image, nil
}

// LoadDecoded resolves and decodes an image source, also reporting its
// intrinsic display size.
func LoadDecoded(src string, opt LoadOptions) (Decoded, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return Decoded{}, fmt.Errorf("empty image source")
	}

	data, isSVG, err := readSource(src, opt)
	if err != nil {
		return Decoded{}, err
	}
	if isSVG {
		img, vw, vh, err := rasterizeSVG(data, 0, 0)
		if err != nil {
			return Decoded{}, err
		}
		return Decoded{Image: img, DisplayW: vw, DisplayH: vh}, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Decoded{}, fmt.Errorf("decode image: %w", err)
	}
	b := img.Bounds()
	return Decoded{Image: img, DisplayW: b.Dx(), DisplayH: b.Dy()}, nil
}

// readSource returns the raw bytes for src plus whether the content is SVG
// (which image.Decode cannot handle and must be rasterized).
func readSource(src string, opt LoadOptions) (data []byte, isSVG bool, err error) {
	switch {
	case strings.HasPrefix(src, "data:"):
		return readDataURI(src)
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		if !opt.AllowRemote {
			return nil, false, fmt.Errorf("remote images disabled")
		}
		return readRemote(src)
	default:
		return readFile(src, opt.BaseDir)
	}
}

func readDataURI(src string) ([]byte, bool, error) {
	// data:[<mediatype>][;base64],<data>
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, false, fmt.Errorf("malformed data URI")
	}
	meta := src[5:comma]
	payload := src[comma+1:]
	isSVG := strings.Contains(meta, "image/svg")

	var raw []byte
	if strings.Contains(meta, ";base64") {
		dec, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, false, fmt.Errorf("decode data URI: %w", err)
		}
		raw = dec
	} else {
		if unesc, err := url.QueryUnescape(payload); err == nil {
			raw = []byte(unesc)
		} else {
			raw = []byte(payload)
		}
	}
	if len(raw) > maxImageBytes {
		return nil, false, fmt.Errorf("image exceeds size limit")
	}
	return raw, isSVG || looksLikeSVG(raw), nil
}

func readFile(src, baseDir string) ([]byte, bool, error) {
	// Strip an optional inline title or fragment.
	if i := strings.IndexAny(src, " \t"); i >= 0 {
		src = src[:i]
	}
	p := src
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, false, err
	}
	if info.Size() > maxImageBytes {
		return nil, false, fmt.Errorf("image exceeds size limit")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false, err
	}
	isSVG := strings.EqualFold(filepath.Ext(p), ".svg") || looksLikeSVG(data)
	return data, isSVG, nil
}

func readRemote(src string) ([]byte, bool, error) {
	client := &http.Client{Timeout: remoteTimeout}
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "mdv")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("fetch image: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxImageBytes {
		return nil, false, fmt.Errorf("image exceeds size limit")
	}
	ct := resp.Header.Get("Content-Type")
	isSVG := strings.Contains(ct, "image/svg") ||
		strings.EqualFold(filepath.Ext(pathOf(src)), ".svg") ||
		looksLikeSVG(data)
	return data, isSVG, nil
}

func pathOf(rawurl string) string {
	if u, err := url.Parse(rawurl); err == nil {
		return u.Path
	}
	return rawurl
}

// looksLikeSVG sniffs the leading bytes for an SVG root or XML declaration.
func looksLikeSVG(data []byte) bool {
	head := data
	if len(head) > 256 {
		head = head[:256]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<svg") || (strings.Contains(s, "<?xml") && strings.Contains(strings.ToLower(string(data)), "<svg"))
}

// rasterizeSVG renders SVG bytes into a raster image and reports the SVG's
// intrinsic viewBox size (used for GUI-like relative sizing). When w or h is
// zero the viewBox size is used. A high-resolution baseline is enforced so the
// result stays crisp when later scaled to fit a column.
func rasterizeSVG(data []byte, w, h int) (img image.Image, viewW, viewH int, err error) {
	// IgnoreErrorMode keeps oksvg silent about unsupported elements (e.g. the
	// <text> in shields.io badges) instead of logging a warning per element.
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse SVG: %w", err)
	}
	vw := icon.ViewBox.W
	vh := icon.ViewBox.H
	if vw <= 0 || vh <= 0 {
		vw, vh = 300, 150
	}
	// Choose a render size: honor explicit dimensions, else upscale the viewBox
	// to a minimum baseline so downstream fitting does not look blocky.
	const minBaseline = 600.0
	rw, rh := float64(w), float64(h)
	if rw <= 0 || rh <= 0 {
		scale := 1.0
		if vw < minBaseline {
			scale = minBaseline / vw
		}
		rw = vw * scale
		rh = vh * scale
	}
	iw, ih := int(rw+0.5), int(rh+0.5)
	if iw < 1 {
		iw = 1
	}
	if ih < 1 {
		ih = 1
	}
	if iw > 4096 {
		iw = 4096
	}
	if ih > 4096 {
		ih = 4096
	}

	icon.SetTarget(0, 0, float64(iw), float64(ih))
	rgba := image.NewRGBA(image.Rect(0, 0, iw, ih))
	scanner := rasterx.NewScannerGV(int(vw), int(vh), rgba, rgba.Bounds())
	raster := rasterx.NewDasher(iw, ih, scanner)
	icon.Draw(raster, 1.0)
	return rgba, int(vw + 0.5), int(vh + 0.5), nil
}

// fit scales img to fit within maxW×maxH pixels, preserving aspect ratio, using
// high-quality resampling. It never upscales beyond the source. When maxH is
// zero only width constrains the result.
func fit(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || maxW <= 0 {
		return img
	}
	scale := float64(maxW) / float64(sw)
	if maxH > 0 {
		if s := float64(maxH) / float64(sh); s < scale {
			scale = s
		}
	}
	if scale >= 1 {
		return img // do not upscale; keep native pixels
	}
	dw := int(float64(sw)*scale + 0.5)
	dh := int(float64(sh)*scale + 0.5)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// resize scales img to exactly w×h pixels with high-quality resampling.
func resize(img image.Image, w, h int) image.Image {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}
