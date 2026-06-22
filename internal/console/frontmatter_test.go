package console

import (
	"strings"
	"testing"
)

func TestRenderFrontmatterShown(t *testing.T) {
	t.Setenv("NO_COLOR", "1") // deterministic, ANSI-free output

	var sb strings.Builder
	md := "---\ntitle: My Doc\nauthor: Jane\ndate: 2024-05-06\ntags: [go, cli]\nstatus: draft\n---\n# Heading\n\nBody text.\n"
	if err := Render(&sb, md, "doc.md", Options{Width: 80}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()

	for _, want := range []string{"My Doc", "Jane", "2024-05-06", "#go", "#cli", "status", "draft"} {
		if !strings.Contains(out, want) {
			t.Errorf("front matter output missing %q in:\n%s", want, out)
		}
	}
	// The raw closing fence must not leak into the rendered body.
	if strings.Contains(out, "\n---\n") {
		t.Errorf("raw front matter fence leaked into output:\n%s", out)
	}
	if !strings.Contains(out, "Heading") || !strings.Contains(out, "Body text.") {
		t.Errorf("body content missing:\n%s", out)
	}
}

func TestRenderNoFrontmatterUnchanged(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var sb strings.Builder
	md := "# Plain\n\nNo metadata here.\n"
	if err := Render(&sb, md, "doc.md", Options{Width: 80}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()
	if strings.Contains(out, "─") {
		t.Errorf("unexpected metadata separator for doc without front matter:\n%s", out)
	}
}
