package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{50 << 20, "50.0 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReadMarkdownFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	// One byte over the limit is enough to trigger the guard.
	if err := os.Truncate(createSized(t, path), MaxFileBytes+1); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	_, err := ReadMarkdownFile(path)
	var tooLarge *FileTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *FileTooLargeError, got %v", err)
	}
	if !strings.Contains(tooLarge.Error(), "too large") {
		t.Errorf("message missing 'too large': %q", tooLarge.Error())
	}
	if !strings.Contains(tooLarge.Error(), "big.md") {
		t.Errorf("message should name the file: %q", tooLarge.Error())
	}
}

func TestReadMarkdownFileOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.md")
	if err := os.WriteFile(path, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadMarkdownFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "# hi\n" {
		t.Errorf("unexpected content: %q", data)
	}
}

func TestResolveInputTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	if err := os.Truncate(createSized(t, path), MaxFileBytes+1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	_, err := ResolveInput(path)
	var tooLarge *FileTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *FileTooLargeError, got %v", err)
	}
}

// createSized creates an empty file and returns its path, ready to be grown
// with os.Truncate (a sparse grow that costs no real disk space).
func createSized(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
