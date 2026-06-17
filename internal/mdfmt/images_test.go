package mdfmt

import (
	"strings"
	"testing"
)

// fakeImageRenderer renders any source into a fixed sentinel block, recording
// the sources it was asked to render.
type fakeImageRenderer struct {
	calls   []string
	widths  []int
	failFor map[string]bool
}

func (f *fakeImageRenderer) RenderImage(src, alt string, colWidth, dispW int) (string, bool) {
	f.calls = append(f.calls, src)
	f.widths = append(f.widths, dispW)
	if f.failFor[src] {
		return "", false
	}
	return "<<IMG:" + src + ">>", true
}

// rowImageRenderer also implements imageRowRenderer, recording row groupings.
type rowImageRenderer struct {
	fakeImageRenderer
	rows [][]string
}

func (f *rowImageRenderer) RenderImageRow(srcs []string, width int) (string, bool) {
	f.rows = append(f.rows, srcs)
	return "<<ROW:" + strings.Join(srcs, ",") + ">>", true
}

func TestRenderLaysConsecutiveImagesInRow(t *testing.T) {
	f := &rowImageRenderer{}
	in := "[![A](a.svg)](l1)\n[![B](b.svg)](l2)\n[![C](c.svg)](l3)\n"
	out, err := Render(in, 80, "notty", false, f)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.rows) != 1 {
		t.Fatalf("RenderImageRow called %d times, want 1; rows=%v", len(f.rows), f.rows)
	}
	want := []string{"a.svg", "b.svg", "c.svg"}
	if strings.Join(f.rows[0], ",") != strings.Join(want, ",") {
		t.Errorf("row sources = %v, want %v", f.rows[0], want)
	}
	if !strings.Contains(out, "<<ROW:a.svg,b.svg,c.svg>>") {
		t.Errorf("row block not substituted: %q", out)
	}
	// A row of images must not also be rendered as singles.
	if len(f.calls) != 0 {
		t.Errorf("RenderImage should not be called for a row, got %v", f.calls)
	}
}

func TestRenderLoneImageNotTreatedAsRow(t *testing.T) {
	f := &rowImageRenderer{}
	in := "![Hero](hero.png)\n\nText.\n"
	if _, err := Render(in, 80, "notty", false, f); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.rows) != 0 {
		t.Errorf("single image should not use RenderImageRow, rows=%v", f.rows)
	}
	if len(f.calls) != 1 || f.calls[0] != "hero.png" {
		t.Errorf("RenderImage calls = %v, want [hero.png]", f.calls)
	}
}

func TestRenderPassesHTMLWidthAttr(t *testing.T) {
	f := &fakeImageRenderer{}
	in := `<img src="hero.png" alt="Icon" width="400" />` + "\n"
	if _, err := Render(in, 80, "notty", false, f); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.widths) != 1 || f.widths[0] != 400 {
		t.Errorf("display widths = %v, want [400]", f.widths)
	}
}

func TestRenderPassesMarkdownSizeHint(t *testing.T) {
	f := &fakeImageRenderer{}
	in := "![Hero](hero.png =640x480)\n"
	if _, err := Render(in, 80, "notty", false, f); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.widths) != 1 || f.widths[0] != 640 {
		t.Errorf("display widths = %v, want [640]", f.widths)
	}
}

func TestRenderNoWidthHintIsZero(t *testing.T) {
	f := &fakeImageRenderer{}
	in := "![Hero](hero.png)\n"
	if _, err := Render(in, 80, "notty", false, f); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.widths) != 1 || f.widths[0] != 0 {
		t.Errorf("display widths = %v, want [0]", f.widths)
	}
}

func TestRenderSubstitutesStandaloneMarkdownImage(t *testing.T) {
	f := &fakeImageRenderer{}
	in := "# Title\n\n![Hero](images/hero.png)\n\nBody text.\n"
	out, err := Render(in, 80, "notty", false, f)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "images/hero.png" {
		t.Fatalf("RenderImage calls = %v, want [images/hero.png]", f.calls)
	}
	if !strings.Contains(out, "<<IMG:images/hero.png>>") {
		t.Errorf("image block not substituted into output: %q", out)
	}
	if !strings.Contains(out, "Body text.") {
		t.Errorf("surrounding text lost: %q", out)
	}
}

func TestRenderSubstitutesHTMLImage(t *testing.T) {
	f := &fakeImageRenderer{}
	in := `<div align="center"><a href="https://x"><img src="logo.svg" alt="Logo" width="800" /></a></div>` + "\n"
	out, err := Render(in, 80, "notty", false, f)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != "logo.svg" {
		t.Fatalf("RenderImage calls = %v, want [logo.svg]", f.calls)
	}
	if !strings.Contains(out, "<<IMG:logo.svg>>") {
		t.Errorf("HTML image block not substituted: %q", out)
	}
}

func TestRenderKeepsAltWhenImageUnrenderable(t *testing.T) {
	f := &fakeImageRenderer{failFor: map[string]bool{"missing.png": true}}
	in := "![Some Alt](missing.png)\n"
	out, err := Render(in, 80, "notty", false, f)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "<<IMG:") {
		t.Errorf("unexpected image block for failed render: %q", out)
	}
	if !strings.Contains(out, "Some Alt") {
		t.Errorf("alt text not preserved on fallback: %q", out)
	}
}

func TestRenderImageInCodeBlockIsNotRendered(t *testing.T) {
	f := &fakeImageRenderer{}
	in := "```\n![x](y.png)\n```\n"
	_, err := Render(in, 80, "notty", false, f)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("image inside code block should not render, calls = %v", f.calls)
	}
}
