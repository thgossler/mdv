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
		got := ResolveLink(c.raw, dir, dir, cfg, nil)
		if got.Kind != c.want {
			t.Errorf("ResolveLink(%q).Kind = %v, want %v", c.raw, got.Kind, c.want)
		}
	}

	// Root-relative link ("/other.md") falls back to the workspace root when it
	// does not exist as a literal absolute filesystem path.
	if got := ResolveLink("/other.md", dir, dir, cfg, nil); got.Kind != LinkMarkdown || got.Resolved != mdPath {
		t.Errorf("root-relative link: %+v", got)
	}

	// Fragment extraction.
	if got := ResolveLink("other.md#features", dir, dir, cfg, nil); got.Fragment != "features" {
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

// TestResolveLinkUpwardAndAnchor covers parent-directory traversal combined
// with a cross-file heading anchor (e.g. "../guide/setup.md#install").
func TestResolveLinkUpwardAndAnchor(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSettings()

	guide := filepath.Join(dir, "guide")
	if err := os.MkdirAll(guide, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := filepath.Join(guide, "setup.md")
	if err := os.WriteFile(setup, []byte("# Setup\n\n## Install\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Link issued from a sibling directory using an upward reference + anchor.
	sub := filepath.Join(dir, "topics")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got := ResolveLink("../guide/setup.md#install", sub, dir, cfg, nil)
	if got.Kind != LinkMarkdown {
		t.Fatalf("upward link kind = %v, want markdown", got.Kind)
	}
	if got.Resolved != setup {
		t.Errorf("upward link resolved = %q, want %q", got.Resolved, setup)
	}
	if got.Fragment != "install" {
		t.Errorf("upward link fragment = %q, want %q", got.Fragment, "install")
	}
}

// TestResolveLinkCaseInsensitive verifies that a link whose letter case differs
// from the real file still resolves. On case-sensitive filesystems this
// exercises the case-insensitive fallback; on case-insensitive ones os.Stat
// already matches. Either way the link must not be reported as broken.
func TestResolveLinkCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSettings()

	real := filepath.Join(dir, "guide", "Setup.md")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("# Setup"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Differing case in both the directory and file component.
	if got := ResolveLink("GUIDE/setup.md", dir, dir, cfg, nil); got.Kind != LinkMarkdown {
		t.Errorf("case-insensitive link kind = %v, want markdown (%+v)", got.Kind, got)
	}
	// URL-encoded + case-differing path.
	if got := ResolveLink("guide/Setup.md#features", dir, dir, cfg, nil); got.Kind != LinkMarkdown || got.Fragment != "features" {
		t.Errorf("encoded+case link: %+v", got)
	}
}

// TestResolveLinkDirectoryIndex verifies that a link to a directory resolves to
// its README/index landing page using the configured markdown extensions.
func TestResolveLinkDirectoryIndex(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSettings()

	// Directory with a README.
	withReadme := filepath.Join(dir, "withReadme")
	if err := os.MkdirAll(withReadme, 0o755); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(withReadme, "README.md")
	if err := os.WriteFile(readme, []byte("# R"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLink("withReadme", dir, dir, cfg, nil); got.Kind != LinkMarkdown || got.Resolved != readme {
		t.Errorf("directory->README: %+v, want %q", got, readme)
	}

	// Directory with only an index document (non-.md markdown extension).
	withIndex := filepath.Join(dir, "withIndex")
	if err := os.MkdirAll(withIndex, 0o755); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(withIndex, "index.markdown")
	if err := os.WriteFile(index, []byte("# I"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLink("withIndex", dir, dir, cfg, nil); got.Kind != LinkMarkdown || got.Resolved != index {
		t.Errorf("directory->index: %+v, want %q", got, index)
	}

	// Empty directory stays an external (OS-opened) target.
	empty := filepath.Join(dir, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLink("empty", dir, dir, cfg, nil); got.Kind != LinkExternalFile {
		t.Errorf("empty directory kind = %v, want file", got.Kind)
	}
}

// TestResolveLinkSymlinks verifies that symlinked markdown files resolve through
// the link and that symlink loops degrade to a broken link instead of hanging.
func TestResolveLinkSymlinks(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSettings()

	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, []byte("# Real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if got := ResolveLink("alias.md", dir, dir, cfg, nil); got.Kind != LinkMarkdown {
		t.Errorf("symlinked markdown kind = %v, want markdown", got.Kind)
	}

	// Symlink loop: a -> b -> a. os.Stat returns ELOOP, so the link is broken.
	a := filepath.Join(dir, "loopA.md")
	b := filepath.Join(dir, "loopB.md")
	if err := os.Symlink(b, a); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if got := ResolveLink("loopA.md", dir, dir, cfg, nil); got.Kind != LinkBroken {
		t.Errorf("symlink loop kind = %v, want broken", got.Kind)
	}
}
