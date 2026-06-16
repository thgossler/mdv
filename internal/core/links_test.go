package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLinkKinds(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSettings()

	// Create a markdown file and a non-markdown file to resolve against.
	mdPath := filepath.Join(dir, "other.md")
	if err := os.WriteFile(mdPath, []byte("# Other"), 0o644); err != nil {
		t.Fatal(err)
	}
	txtPath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(txtPath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		raw  string
		want LinkKind
	}{
		{"#section", LinkAnchor},
		{"https://example.com", LinkHTTP},
		{"http://example.com/path", LinkHTTP},
		{"mailto:a@b.com", LinkMailto},
		{"other.md", LinkMarkdown},
		{"other.md#heading", LinkMarkdown},
		{"./other", LinkMarkdown}, // extension inferred
		{"data.txt", LinkExternalFile},
		{"missing.md", LinkBroken},
		{"//cdn.example.com/x", LinkHTTP},
	}
	for _, c := range cases {
		got := ResolveLink(c.raw, dir, cfg, nil)
		if got.Kind != c.want {
			t.Errorf("ResolveLink(%q).Kind = %v, want %v", c.raw, got.Kind, c.want)
		}
	}

	// Fragment extraction.
	if got := ResolveLink("other.md#features", dir, cfg, nil); got.Fragment != "features" {
		t.Errorf("fragment = %q, want %q", got.Fragment, "features")
	}
}

func TestResolveWikilink(t *testing.T) {
	cfg := DefaultSettings()
	ws := []DocFile{
		{Path: "/notes/Setup Guide.md", Name: "Setup Guide.md", Title: "Setup Guide"},
		{Path: "/notes/docs/api.md", Name: "api.md"},
	}

	if got := ResolveWikilink("Setup Guide", cfg, ws); got.Kind != LinkWikiInternal {
		t.Errorf("wikilink by name: kind = %v", got.Kind)
	}
	if got := ResolveWikilink("api|API Reference", cfg, ws); got.Kind != LinkWikiInternal || got.Resolved != "/notes/docs/api.md" {
		t.Errorf("wikilink with alias: %+v", got)
	}
	if got := ResolveWikilink("nope", cfg, ws); got.Kind != LinkBroken {
		t.Errorf("broken wikilink: kind = %v", got.Kind)
	}
	if got := ResolveWikilink("api#methods", cfg, ws); got.Fragment != "methods" {
		t.Errorf("wikilink fragment = %q", got.Fragment)
	}
}
