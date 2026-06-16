package console

import (
	"strings"
	"testing"
)

func TestRenderPlainText(t *testing.T) {
	t.Setenv("NO_COLOR", "1") // force the "notty" style for deterministic output

	var sb strings.Builder
	md := "# Title\n\nSome **bold** text and a list:\n\n- one\n- two\n"
	if err := Render(&sb, md, "doc.md", Options{Width: 80}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "Title") {
		t.Errorf("rendered output missing heading text: %q", out)
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Errorf("rendered output missing list items: %q", out)
	}
	// With NO_COLOR there must be no ANSI escape sequences.
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR output still contains ANSI escapes: %q", out)
	}
}

func TestRenderHeaderAndFile(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var sb strings.Builder
	if err := Render(&sb, "# Hi\n", "/tmp/readme.md", Options{Width: 80, ShowHeader: true}); err != nil {
		t.Fatalf("Render with header: %v", err)
	}
	if !strings.Contains(sb.String(), "/tmp/readme.md") {
		t.Errorf("ShowHeader did not print the path: %q", sb.String())
	}
}
