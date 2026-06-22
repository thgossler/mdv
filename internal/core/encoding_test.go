package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeToUTF8 verifies that BOM-prefixed UTF-8 and UTF-16 (LE/BE) input is
// normalized to plain UTF-8, while content without a recognized BOM is returned
// unchanged.
func TestDecodeToUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{
			name: "plain utf-8 unchanged",
			in:   []byte("# Hello\nworld"),
			want: "# Hello\nworld",
		},
		{
			name: "utf-8 bom stripped",
			in:   append([]byte{0xEF, 0xBB, 0xBF}, []byte("# Hi")...),
			want: "# Hi",
		},
		{
			name: "utf-16 le with bom",
			in:   []byte{0xFF, 0xFE, 0x48, 0x00, 0x69, 0x00},
			want: "Hi",
		},
		{
			name: "utf-16 be with bom",
			in:   []byte{0xFE, 0xFF, 0x00, 0x48, 0x00, 0x69},
			want: "Hi",
		},
		{
			name: "empty unchanged",
			in:   []byte{},
			want: "",
		},
		{
			name: "single byte unchanged",
			in:   []byte{0xEF},
			want: "\xEF",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(DecodeToUTF8(tt.in))
			if got != tt.want {
				t.Errorf("DecodeToUTF8 = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestReadMarkdownFileNormalizesEncoding confirms ReadMarkdownFile decodes a
// UTF-16 LE file (with BOM) to UTF-8 so downstream renderers see plain text.
func TestReadMarkdownFileNormalizesEncoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utf16.md")
	// "# Hi" encoded as UTF-16 LE with a leading BOM.
	data := []byte{0xFF, 0xFE, '#', 0x00, ' ', 0x00, 'H', 0x00, 'i', 0x00}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadMarkdownFile(path)
	if err != nil {
		t.Fatalf("ReadMarkdownFile: %v", err)
	}
	if string(got) != "# Hi" {
		t.Errorf("ReadMarkdownFile = %q, want %q", string(got), "# Hi")
	}
	if strings.ContainsRune(string(got), '\uFEFF') {
		t.Errorf("decoded output still contains a BOM: %q", string(got))
	}
}
