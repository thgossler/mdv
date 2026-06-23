package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePDFOutputPath(t *testing.T) {
	dir := t.TempDir()

	t.Run("existing folder uses default name from input", func(t *testing.T) {
		out, err := ResolvePDFOutputPath(dir, "README.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := filepath.Join(dir, "README.md.pdf"); out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
	})

	t.Run("trailing separator is treated as folder", func(t *testing.T) {
		sub := filepath.Join(dir, "sub") + string(os.PathSeparator)
		out, err := ResolvePDFOutputPath(sub, "notes.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := filepath.Join(dir, "sub", "notes.md.pdf"); out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
		if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
			t.Fatalf("expected folder to be created: %v", err)
		}
	})

	t.Run("stdin uses document.pdf default", func(t *testing.T) {
		out, err := ResolvePDFOutputPath(dir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := filepath.Join(dir, "document.pdf"); out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
	})

	t.Run("explicit .pdf file is used as-is", func(t *testing.T) {
		arg := filepath.Join(dir, "custom.pdf")
		out, err := ResolvePDFOutputPath(arg, "README.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != arg {
			t.Fatalf("got %q, want %q", out, arg)
		}
	})

	t.Run("file without extension gets .pdf", func(t *testing.T) {
		arg := filepath.Join(dir, "report")
		out, err := ResolvePDFOutputPath(arg, "README.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := arg + ".pdf"; out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
	})

	t.Run("file with non-pdf extension keeps name and appends .pdf", func(t *testing.T) {
		arg := filepath.Join(dir, "notes.md")
		out, err := ResolvePDFOutputPath(arg, "README.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := arg + ".pdf"; out != want {
			t.Fatalf("got %q, want %q", out, want)
		}
	})

	t.Run("PDF extension is case-insensitive", func(t *testing.T) {
		arg := filepath.Join(dir, "Custom.PDF")
		out, err := ResolvePDFOutputPath(arg, "README.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != arg {
			t.Fatalf("got %q, want %q", out, arg)
		}
	})

	t.Run("relative path resolves against CWD", func(t *testing.T) {
		out, err := ResolvePDFOutputPath("out.pdf", "README.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !filepath.IsAbs(out) {
			t.Fatalf("expected absolute path, got %q", out)
		}
		if filepath.Base(out) != "out.pdf" {
			t.Fatalf("got base %q, want out.pdf", filepath.Base(out))
		}
	})

	t.Run("empty argument is an error", func(t *testing.T) {
		if _, err := ResolvePDFOutputPath("  ", "README.md"); err == nil {
			t.Fatalf("expected error for empty argument")
		}
	})
}
