package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBacklinks(t *testing.T) {
	cfg := DefaultSettings()
	dir := t.TempDir()

	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	target := write("target.md", "# Target\n")
	srcInline := write("a.md", "See [the target](target.md) for details.\n")
	srcWiki := write("b.md", "Refer to [[target]] here.\n")
	srcFenced := write("c.md", "```\n[fake](target.md)\n```\nno real link\n")
	srcRooted := write("e.md", "Go to the [index](/target.md) page.\n")
	_ = write("d.md", "Unrelated [link](https://example.com).\n")

	workspace := []DocFile{
		{Path: target, Name: "target.md"},
		{Path: srcInline, Name: "a.md"},
		{Path: srcWiki, Name: "b.md"},
		{Path: srcFenced, Name: "c.md"},
		{Path: srcRooted, Name: "e.md"},
		{Path: filepath.Join(dir, "d.md"), Name: "d.md"},
	}

	got := FindBacklinks(target, dir, cfg, workspace)

	found := map[string]bool{}
	for _, b := range got {
		found[b.SourceName] = true
		if b.Line <= 0 || b.Snippet == "" {
			t.Errorf("backlink missing line/snippet: %+v", b)
		}
	}

	if !found["a.md"] {
		t.Error("inline markdown link not detected as a backlink")
	}
	if !found["b.md"] {
		t.Error("wikilink not detected as a backlink")
	}
	if !found["e.md"] {
		t.Error("root-relative link not detected as a backlink")
	}
	if found["c.md"] {
		t.Error("link inside a code fence must not count as a backlink")
	}
	if found["d.md"] {
		t.Error("unrelated external link must not count as a backlink")
	}
	if found["target.md"] {
		t.Error("a document must not back-link to itself")
	}
}
