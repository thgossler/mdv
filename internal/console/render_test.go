package console

import (
	"os"
	"path/filepath"
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

func TestDetectStyleNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := detectStyle(); got != "notty" {
		t.Errorf("detectStyle() with NO_COLOR = %q, want notty", got)
	}
}

func TestDetectStyleNonTTY(t *testing.T) {
	t.Setenv("NO_COLOR", "") // clear; stdout under `go test` is not a terminal
	if got := detectStyle(); got != "notty" {
		t.Errorf("detectStyle() non-TTY = %q, want notty", got)
	}
}

func TestDetectWidthNonTTY(t *testing.T) {
	// Under `go test` stdout is not a terminal, so detectWidth falls back to 80.
	if got := detectWidth(); got != 80 {
		t.Errorf("detectWidth() non-TTY = %d, want 80", got)
	}
}

func TestTTYHelpersNonInteractive(t *testing.T) {
	// Under `go test` neither stdout nor stdin is an interactive terminal.
	if StdoutIsTTY() {
		t.Error("StdoutIsTTY() = true under go test, want false")
	}
	if StdinIsTTY() {
		t.Error("StdinIsTTY() = true under go test, want false")
	}
}

func TestRenderFile(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("# Heading\n\nbody text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	if err := RenderFile(&sb, path, Options{Width: 80}); err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if !strings.Contains(sb.String(), "Heading") {
		t.Errorf("RenderFile output missing heading: %q", sb.String())
	}
}

func TestRenderFileNotFound(t *testing.T) {
	var sb strings.Builder
	err := RenderFile(&sb, filepath.Join(t.TempDir(), "missing.md"), Options{Width: 80})
	if err == nil {
		t.Error("RenderFile on missing file returned nil error, want error")
	}
}
