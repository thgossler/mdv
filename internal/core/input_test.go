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

func TestListMarkdownFilesExcludePatterns(t *testing.T) {
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
	must("draft-ideas.md", "# Draft")
	must("archive/old.md", "# Old")
	must("archive/notes.md", "# Notes")

	tests := []struct {
		name     string
		patterns []string
		wantRels []string
	}{
		{
			name:     "no patterns returns all files",
			patterns: nil,
			// sortDocFiles: depth then alpha; README suffix trick places it after
			// alpha/draft at the same depth.
			wantRels: []string{"alpha.md", "draft-ideas.md", "README.md", "archive/notes.md", "archive/old.md"},
		},
		{
			name:     "glob excludes matching files",
			patterns: []string{"draft*"},
			wantRels: []string{"alpha.md", "README.md", "archive/notes.md", "archive/old.md"},
		},
		{
			name:     "directory pattern excludes entire subtree",
			patterns: []string{"archive/**"},
			wantRels: []string{"alpha.md", "draft-ideas.md", "README.md"},
		},
		{
			name:     "multiple patterns are combined",
			patterns: []string{"draft*", "archive/**"},
			wantRels: []string{"alpha.md", "README.md"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultSettings()
			cfg.ExcludePatterns = tc.patterns
			files, err := ListMarkdownFiles(root, cfg)
			if err != nil {
				t.Fatalf("ListMarkdownFiles: %v", err)
			}
			if len(files) != len(tc.wantRels) {
				got := make([]string, len(files))
				for i, f := range files {
					got[i] = f.Rel
				}
				t.Fatalf("got %d files %v, want %d %v", len(files), got, len(tc.wantRels), tc.wantRels)
			}
			for i, f := range files {
				if f.Rel != tc.wantRels[i] {
					t.Errorf("files[%d].Rel = %q, want %q", i, f.Rel, tc.wantRels[i])
				}
			}
		})
	}
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

	files := []DocFile{{Path: atx, Name: "atx.md"}, {Path: setext, Name: "setext.md"}, {Path: none, Name: "What-is-the-platform%3F.md"}}
	PopulateTitles(files)
	if files[0].Title != "Real Title" || files[1].Title != "My Heading" {
		t.Errorf("PopulateTitles = %+v", files)
	}
	// A document with no detectable heading falls back to a sanitized file name.
	if files[2].Title != "What is the platform?" {
		t.Errorf("PopulateTitles fallback = %q, want %q", files[2].Title, "What is the platform?")
	}
}

func TestFilenameTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"For-All-Partners-(UNrestricted)/Services.md", "Services"},
		{"What-is-the-platform%3F.md", "What is the platform?"},
		{"Getting-Started-with-teamplay.md", "Getting Started with teamplay"},
		{"docs/Collected-Q&A.md", "Collected Q&A"},
		{"a%20b%20c.markdown", "a b c"},
		{`C:\Users\me\My-Notes.md`, "My Notes"},
		{"README", "README"},
		{"%ZZbad-name.md", "%ZZbad name"},
	}
	for _, c := range cases {
		if got := FilenameTitle(c.in); got != c.want {
			t.Errorf("FilenameTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsSkippedDir(t *testing.T) {
	for _, name := range []string{".git", "node_modules", ".hidden", ".svn"} {
		if !IsSkippedDir(name) {
			t.Errorf("IsSkippedDir(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"docs", "src", "wiki", "documentation"} {
		if IsSkippedDir(name) {
			t.Errorf("IsSkippedDir(%q) = true, want false", name)
		}
	}
}

func TestWorkspaceDirs(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("docs/guide")
	mk("src")
	mk("node_modules/pkg") // skipped (noise dir)
	mk(".git/objects")     // skipped (hidden dir)

	got := WorkspaceDirs(root)

	want := map[string]bool{
		root:                                 true,
		filepath.Join(root, "docs"):          true,
		filepath.Join(root, "docs", "guide"): true,
		filepath.Join(root, "src"):           true,
	}
	if len(got) != len(want) {
		t.Fatalf("WorkspaceDirs returned %d dirs, want %d: %v", len(got), len(want), got)
	}
	for _, d := range got {
		if !want[d] {
			t.Errorf("unexpected watched dir %q (noise/hidden trees must be skipped)", d)
		}
	}
}
