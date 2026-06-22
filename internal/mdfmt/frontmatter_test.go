package mdfmt

import (
	"strings"
	"testing"

	"github.com/thgossler/mdv/internal/core"
)

func TestRenderFrontmatterPlain(t *testing.T) {
	fm, _ := core.ExtractFrontmatter("---\ntitle: T\nauthor: A\ndate: 2024-01-01\ntags: [x, y]\nextra: v\n---\nbody\n")
	out := RenderFrontmatter(fm, FrontmatterOptions{Width: 30, Color: false, ShowFields: true})

	for _, want := range []string{"T", "A", "2024-01-01", "#x", "#y", "extra", "v"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain mode produced ANSI escapes:\n%q", out)
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("block should end with a blank line: %q", out)
	}
}

func TestRenderFrontmatterCollapsedHidesFields(t *testing.T) {
	fm, _ := core.ExtractFrontmatter("---\ntitle: T\nextra: secret\n---\nbody\n")
	out := RenderFrontmatter(fm, FrontmatterOptions{Width: 20, Color: false, ShowFields: false, Hint: "m: more"})
	if strings.Contains(out, "secret") {
		t.Errorf("collapsed block leaked field value:\n%s", out)
	}
	if !strings.Contains(out, "m: more") {
		t.Errorf("hint missing:\n%s", out)
	}
}

func TestRenderFrontmatterColorOn(t *testing.T) {
	fm, _ := core.ExtractFrontmatter("---\ntitle: T\n---\nbody\n")
	out := RenderFrontmatter(fm, FrontmatterOptions{Width: 10, Color: true})
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("color mode produced no ANSI escapes:\n%q", out)
	}
}

func TestRenderFrontmatterNone(t *testing.T) {
	fm, _ := core.ExtractFrontmatter("# no front matter\n")
	if out := RenderFrontmatter(fm, FrontmatterOptions{Width: 40}); out != "" {
		t.Errorf("expected empty block, got %q", out)
	}
}

func TestRenderStripsFrontmatter(t *testing.T) {
	out, err := Render("---\ntitle: X\n---\n# Body\n", 80, "notty", false, nil, "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "title: X") || strings.Contains(out, "---") {
		t.Errorf("front matter not stripped from body:\n%s", out)
	}
	if !strings.Contains(out, "Body") {
		t.Errorf("body missing:\n%s", out)
	}
}
