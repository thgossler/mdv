package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInput(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(file, []byte("# Doc"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty argument -> InputNone.
	if in, err := ResolveInput("  "); err != nil || in.Kind != InputNone {
		t.Errorf("empty arg: kind=%v err=%v", in.Kind, err)
	}

	// A file.
	in, err := ResolveInput(file)
	if err != nil {
		t.Fatalf("ResolveInput(file): %v", err)
	}
	if in.Kind != InputFile {
		t.Errorf("file kind = %v, want InputFile", in.Kind)
	}
	if in.Dir != filepath.Dir(in.Path) {
		t.Errorf("file Dir = %q, want %q", in.Dir, filepath.Dir(in.Path))
	}

	// A folder.
	in, err = ResolveInput(dir)
	if err != nil {
		t.Fatalf("ResolveInput(dir): %v", err)
	}
	if in.Kind != InputFolder || in.Dir != in.Path {
		t.Errorf("folder input = %+v", in)
	}

	// A missing path returns an error.
	if _, err := ResolveInput(filepath.Join(dir, "nope.md")); err == nil {
		t.Error("expected error for missing path")
	}
}

func TestIsMarkdownPath(t *testing.T) {
	cfg := DefaultSettings()
	yes := []string{"a.md", "B.MARKDOWN", "notes.mdown", "x.mkd", "diagram.mmd"}
	no := []string{"a.txt", "b.go", "c", "d.html"}
	for _, p := range yes {
		if !IsMarkdownPath(p, cfg) {
			t.Errorf("IsMarkdownPath(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsMarkdownPath(p, cfg) {
			t.Errorf("IsMarkdownPath(%q) = true, want false", p)
		}
	}
}

func TestListMarkdownFilesOrderingAndSkips(t *testing.T) {
	cfg := DefaultSettings()
	root := t.TempDir()

	must := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	must("README.md", "# Home")
	must("alpha.md", "# Alpha")
	must("zeta.md", "# Zeta")
	must("docs/guide.md", "# Guide")
	must("notes.txt", "ignored")          // non-markdown
	must(".hidden/secret.md", "# Secret") // hidden dir skipped
	must("node_modules/pkg/readme.md", "# Dep")

	files, err := ListMarkdownFiles(root, cfg)
	if err != nil {
		t.Fatalf("ListMarkdownFiles: %v", err)
	}

	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f.Path))
		if filepath.Base(f.Path) == "secret.md" {
			t.Error("hidden directory was not skipped")
		}
		if filepath.Base(f.Path) == "notes.txt" {
			t.Error("non-markdown file was included")
		}
	}

	// node_modules and hidden dirs are skipped; non-markdown excluded.
	if len(files) != 4 {
		t.Fatalf("found %d markdown files, want 4: %v", len(files), names)
	}
	// Top-level files are listed before files nested in subdirectories.
	last := names[len(names)-1]
	if last != "guide.md" {
		t.Errorf("last file = %q, want nested guide.md", last)
	}
	// Within the top level the README tie-breaks ahead of a same-named file but
	// plain alphabetical order otherwise holds (alpha < readme < zeta).
	if indexIn(names, "alpha.md") > indexIn(names, "zeta.md") {
		t.Errorf("top-level files not alphabetically ordered: %v", names)
	}
}

func indexIn(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return -1
}

func TestExtractTitleAndPopulate(t *testing.T) {
	dir := t.TempDir()

	atx := filepath.Join(dir, "atx.md")
	if err := os.WriteFile(atx, []byte("intro line\n\n# Real **Title** #\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ExtractTitle(atx); got != "Real Title" {
		t.Errorf("ATX title = %q, want %q", got, "Real Title")
	}

	setext := filepath.Join(dir, "setext.md")
	if err := os.WriteFile(setext, []byte("My Heading\n=========\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ExtractTitle(setext); got != "My Heading" {
		t.Errorf("Setext title = %q, want %q", got, "My Heading")
	}

	fenced := filepath.Join(dir, "fenced.md")
	if err := os.WriteFile(fenced, []byte("```\n# Not a title\n```\n\n# Actual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ExtractTitle(fenced); got != "Actual" {
		t.Errorf("fenced title = %q, want %q (heading in code fence must be ignored)", got, "Actual")
	}

	none := filepath.Join(dir, "none.md")
	if err := os.WriteFile(none, []byte("no heading here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ExtractTitle(none); got != "" {
		t.Errorf("no-heading title = %q, want empty", got)
	}

	files := []DocFile{{Path: atx, Name: "atx.md"}, {Path: setext, Name: "setext.md"}}
	PopulateTitles(files)
	if files[0].Title != "Real Title" || files[1].Title != "My Heading" {
		t.Errorf("PopulateTitles = %+v", files)
	}
}
