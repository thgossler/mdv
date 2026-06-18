package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cases := []struct {
		in   string
		want string
	}{
		{"~", home},
		{"~/notes/file.md", filepath.Join(home, "notes/file.md")},
		{"relative/path.md", "relative/path.md"}, // unchanged
		{"/abs/path.md", "/abs/path.md"},         // unchanged
		{"foo~bar", "foo~bar"},                   // tilde not leading
		{"./~/x", "./~/x"},                       // tilde mid-path
		{"~user/notes", "~user/notes"},           // not ~/ prefix
	}
	for _, c := range cases {
		if got := expandHome(c.in); got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodePath(t *testing.T) {
	cases := map[string]string{
		"plain/path.md": "plain/path.md", // no percent, returned as-is
		"a%20b.md":      "a b.md",
		"docs%2Fapi.md": "docs/api.md",
		"caf%C3%A9.md":  "café.md",
		"bad%ZZpercent": "bad%ZZpercent", // invalid escape -> original
		"":              "",
		"100%done":      "100%done", // lone % that fails to decode -> original
	}
	for in, want := range cases {
		if got := decodePath(in); got != want {
			t.Errorf("decodePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanHref(t *testing.T) {
	cases := map[string]string{
		"https://example.com":   "https://example.com",
		"  spaced.md  ":         "spaced.md",
		"<https://example.com>": "https://example.com",
		"url.md \"a title\"":    "url.md", // inline title stripped at first space
		"url.md\twith-tab":      "url.md",
		"":                      "",
		"   ":                   "",
		"<a>":                   "a",
	}
	for in, want := range cases {
		if got := cleanHref(in); got != want {
			t.Errorf("cleanHref(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinkKindString(t *testing.T) {
	cases := map[LinkKind]string{
		LinkUnknown:      "unknown",
		LinkMarkdown:     "markdown",
		LinkAnchor:       "anchor",
		LinkHTTP:         "http",
		LinkExternalFile: "file",
		LinkWikiInternal: "wikilink",
		LinkBroken:       "broken",
		LinkMailto:       "mailto",
		LinkKind(999):    "unknown", // out-of-range -> default
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("LinkKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

func TestSluggerReset(t *testing.T) {
	s := NewSlugger()
	if got := s.Slug("Intro"); got != "intro" {
		t.Fatalf("first Slug = %q, want intro", got)
	}
	if got := s.Slug("Intro"); got != "intro-1" {
		t.Fatalf("dup Slug = %q, want intro-1", got)
	}
	s.Reset()
	// After reset the duplicate tracking is cleared, so the same heading slugs
	// back to its base form.
	if got := s.Slug("Intro"); got != "intro" {
		t.Errorf("after Reset, Slug = %q, want intro", got)
	}
}

func TestSluggerNilAndEmpty(t *testing.T) {
	// A zero-value Slugger (nil seen map) must initialise lazily, not panic.
	var s Slugger
	if got := s.Slug("Hello"); got != "hello" {
		t.Errorf("zero-value Slug = %q, want hello", got)
	}
	// Heading that becomes empty after slugging (punctuation only).
	if got := s.Slug("!!!"); got != "" {
		t.Errorf("punctuation-only Slug = %q, want empty", got)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{
		0:    "0",
		1:    "1",
		9:    "9",
		42:   "42",
		1000: "1000",
		-5:   "-5",
	}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestListMarkdownFilesEmptyAndNoMarkdown(t *testing.T) {
	cfg := DefaultSettings()

	// Empty directory yields no files and no error.
	empty := t.TempDir()
	files, err := ListMarkdownFiles(empty, cfg)
	if err != nil {
		t.Fatalf("empty dir error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty dir returned %d files, want 0", len(files))
	}

	// Directory with only non-markdown files yields no markdown.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err = ListMarkdownFiles(dir, cfg)
	if err != nil {
		t.Fatalf("no-markdown dir error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("no-markdown dir returned %d files, want 0", len(files))
	}
}

func TestExtractTitleEdgeCases(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"atx.md", "# First Heading\n\nbody", "First Heading"},
		{"setext.md", "My Title\n========\n\nbody", "My Title"},
		{"multiple.md", "# One\n\n# Two\n", "One"}, // first H1 wins
		{"empty.md", "", ""},
		{"no-heading.md", "just paragraph text\nmore text", ""},
		{"fenced.md", "```\n# Not A Heading\n```\n\n# Real Heading\n", "Real Heading"},
		{"emphasis.md", "# **Bold** Title\n", "Bold Title"},
		{"hash-only.md", "#\n# After\n", ""}, // bare '#' returns empty immediately
		{"unicode.md", "# Привет Мир\n", "Привет Мир"},
	}
	for _, c := range cases {
		p := write(c.name, c.content)
		if got := ExtractTitle(p); got != c.want {
			t.Errorf("ExtractTitle(%s) = %q, want %q", c.name, got, c.want)
		}
	}

	// Non-existent file yields empty string, not an error/panic.
	if got := ExtractTitle(filepath.Join(dir, "does-not-exist.md")); got != "" {
		t.Errorf("ExtractTitle(missing) = %q, want empty", got)
	}
}

func TestResolveLinkExtraCases(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSettings()

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// File in the parent dir, linked from the sub dir via "..".
	parentDoc := filepath.Join(dir, "parent.md")
	if err := os.WriteFile(parentDoc, []byte("# Parent"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Parent-directory traversal resolves to the real markdown file.
	if got := ResolveLink("../parent.md", sub, dir, cfg, nil); got.Kind != LinkMarkdown || got.Resolved != parentDoc {
		t.Errorf("../parent.md: %+v", got)
	}

	// HTTP URL with a query string stays an http link, query preserved.
	if got := ResolveLink("https://example.com/p?a=b&c=d", dir, dir, cfg, nil); got.Kind != LinkHTTP || got.Resolved != "https://example.com/p?a=b&c=d" {
		t.Errorf("query URL: %+v", got)
	}

	// Custom scheme is treated as an OS-handled http-like link.
	if got := ResolveLink("vscode://file/x", dir, dir, cfg, nil); got.Kind != LinkHTTP {
		t.Errorf("custom scheme kind = %v, want http", got.Kind)
	}

	// Empty href is unknown.
	if got := ResolveLink("", dir, dir, cfg, nil); got.Kind != LinkUnknown {
		t.Errorf("empty href kind = %v, want unknown", got.Kind)
	}

	// Percent-encoded local file path resolves after decoding.
	encoded := filepath.Join(dir, "a b.md")
	if err := os.WriteFile(encoded, []byte("# Spaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLink("a%20b.md", dir, dir, cfg, nil); got.Kind != LinkMarkdown || got.Resolved != encoded {
		t.Errorf("encoded path: %+v", got)
	}
}
