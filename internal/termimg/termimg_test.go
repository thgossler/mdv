package termimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":         ModeAuto,
		"auto":     ModeAuto,
		"Graphics": ModeGraphics,
		"pixel":    ModeGraphics,
		"blocks":   ModeBlocks,
		"BLOCK":    ModeBlocks,
		"off":      ModeOff,
		"none":     ModeOff,
		"nonsense": ModeAuto,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestProtocolString(t *testing.T) {
	cases := map[Protocol]string{
		ProtocolNone:   "none",
		ProtocolBlocks: "blocks",
		ProtocolSixel:  "sixel",
		ProtocolITerm2: "iterm2",
		ProtocolKitty:  "kitty",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Protocol(%d).String() = %q, want %q", p, got, want)
		}
	}
}

// solidImage returns an opaque w×h image filled with c.
func solidImage(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestRenderBlocksProducesCells(t *testing.T) {
	img := solidImage(40, 20, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	res, err := Render(img, ProtocolBlocks, 20)
	if err != nil {
		t.Fatalf("Render blocks: %v", err)
	}
	if res.Cols != 20 {
		t.Errorf("cols = %d, want 20", res.Cols)
	}
	if res.Rows < 1 {
		t.Errorf("rows = %d, want >= 1", res.Rows)
	}
	// One newline between each row.
	if got := strings.Count(res.Text, "\n"); got != res.Rows-1 {
		t.Errorf("newlines = %d, want %d", got, res.Rows-1)
	}
	if !strings.Contains(res.Text, "\u2580") {
		t.Error("expected upper-half block glyph in output")
	}
	if !strings.Contains(res.Text, "\x1b[38;2;200;100;50m") {
		t.Error("expected truecolor foreground escape in output")
	}
}

func TestRenderBlocksCapsHeight(t *testing.T) {
	// Very tall image: rows must be capped, width reduced to keep aspect.
	img := solidImage(10, 4000, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	res, err := Render(img, ProtocolBlocks, 100)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if res.Rows > maxBlockRows {
		t.Errorf("rows = %d, want <= %d", res.Rows, maxBlockRows)
	}
}

func TestComposeRowPacksAndWraps(t *testing.T) {
	mk := func(text string, cols, rows int) RenderResult {
		return RenderResult{Text: text, Cols: cols, Rows: rows}
	}
	// Three 1-row images of width 10. With gap 2 and width 24, two fit on the
	// first visual row (10+2+10=22 <= 24) and the third wraps to a second row.
	imgs := []RenderResult{
		mk("AAAAAAAAAA", 10, 1),
		mk("BBBBBBBBBB", 10, 1),
		mk("CCCCCCCCCC", 10, 1),
	}
	out := composeRow(imgs, 24)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 visual rows, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "AAAAAAAAAA") || !strings.Contains(lines[0], "BBBBBBBBBB") {
		t.Errorf("first row should hold A and B side by side: %q", lines[0])
	}
	if strings.Contains(lines[0], "CCCCCCCCCC") {
		t.Errorf("C should have wrapped off the first row: %q", lines[0])
	}
	if !strings.Contains(lines[1], "CCCCCCCCCC") {
		t.Errorf("second row should hold C: %q", lines[1])
	}
}

func TestRenderITerm2Sequence(t *testing.T) {
	img := solidImage(30, 30, color.RGBA{R: 0, G: 128, B: 255, A: 255})
	res, err := Render(img, ProtocolITerm2, 20)
	if err != nil {
		t.Fatalf("Render iterm2: %v", err)
	}
	if !strings.HasPrefix(res.Text, "\x1b]1337;File=inline=1;") {
		t.Errorf("missing iTerm2 OSC 1337 prefix: %q", res.Text[:min(40, len(res.Text))])
	}
	if !strings.Contains(res.Text, "\x07") {
		t.Error("missing BEL terminator")
	}
}

func TestRenderKittySequence(t *testing.T) {
	img := solidImage(30, 30, color.RGBA{R: 0, G: 128, B: 255, A: 255})
	res, err := Render(img, ProtocolKitty, 20)
	if err != nil {
		t.Fatalf("Render kitty: %v", err)
	}
	if !strings.Contains(res.Text, "\x1b_Ga=T,f=100,") {
		t.Error("missing kitty graphics control sequence")
	}
}

func TestLoadPNGFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(8, 8, color.RGBA{R: 1, G: 2, B: 3, A: 255})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := Load("x.png", LoadOptions{BaseDir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
		t.Errorf("bounds = %v, want 8x8", img.Bounds())
	}
}

func TestLoadSVGData(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><rect width="100" height="50" fill="red"/></svg>`
	uri := "data:image/svg+xml;utf8," + svg
	img, err := Load(uri, LoadOptions{})
	if err != nil {
		t.Fatalf("Load SVG: %v", err)
	}
	if img.Bounds().Dx() < 1 || img.Bounds().Dy() < 1 {
		t.Errorf("empty SVG raster: %v", img.Bounds())
	}
}

func TestLoadRemoteDisabled(t *testing.T) {
	if _, err := Load("https://example.com/x.png", LoadOptions{AllowRemote: false}); err == nil {
		t.Error("expected error when remote images are disabled")
	}
}

func TestRendererCachesAndCenters(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "y.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(20, 20, color.RGBA{R: 9, G: 9, B: 9, A: 255})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRenderer(ProtocolBlocks, dir, false)
	block, ok := r.RenderImage("y.png", "alt", 40, 0)
	if !ok || block == "" {
		t.Fatal("expected rendered block")
	}
	// Centered: first line should start with leading spaces (pad).
	if !strings.HasPrefix(block, " ") {
		t.Error("expected centered block to have leading padding")
	}
	block2, ok2 := r.RenderImage("y.png", "alt", 40, 0)
	if !ok2 || block2 != block {
		t.Error("expected identical cached result")
	}
}

func TestRendererUnknownSourceFails(t *testing.T) {
	r := NewRenderer(ProtocolBlocks, t.TempDir(), false)
	if _, ok := r.RenderImage("does-not-exist.png", "alt", 40, 0); ok {
		t.Error("expected ok=false for missing image")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
