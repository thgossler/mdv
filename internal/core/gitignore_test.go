package core

import "testing"

func TestGitignoreMatcher(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		// Basename match at any depth.
		{"basename root", []string{"README.md"}, "README.md", true},
		{"basename nested", []string{"draft.md"}, "docs/draft.md", true},
		{"basename no match", []string{"draft.md"}, "docs/final.md", false},

		// Extension glob.
		{"glob ext", []string{"*.tmp.md"}, "notes.tmp.md", true},
		{"glob ext nested", []string{"*.tmp.md"}, "a/b/notes.tmp.md", true},
		{"glob ext no match", []string{"*.tmp.md"}, "notes.md", false},
		{"star not partial word", []string{"foo"}, "foobar.md", false},

		// Anchored patterns.
		{"anchored root file", []string{"/README.md"}, "README.md", true},
		{"anchored root file not nested", []string{"/README.md"}, "docs/README.md", false},
		{"anchored path", []string{"docs/guide.md"}, "docs/guide.md", true},
		{"anchored path not elsewhere", []string{"docs/guide.md"}, "x/docs/guide.md", false},

		// Directory patterns (trailing slash) match contents.
		{"dir slash contents", []string{"archive/"}, "archive/old.md", true},
		{"dir slash nested", []string{"archive/"}, "a/archive/old.md", true},
		{"dir slash deep", []string{"archive/"}, "archive/2024/old.md", true},
		{"dir slash not file", []string{"archive/"}, "archive.md", false},

		// Folder name without slash also excludes contents.
		{"folder name contents", []string{"node_modules"}, "node_modules/x.md", true},

		// Double-star.
		{"leading globstar", []string{"**/temp.md"}, "a/b/temp.md", true},
		{"leading globstar root", []string{"**/temp.md"}, "temp.md", true},
		{"middle globstar", []string{"a/**/z.md"}, "a/m/n/z.md", true},
		{"middle globstar direct", []string{"a/**/z.md"}, "a/z.md", true},
		{"trailing globstar", []string{"build/**"}, "build/x/y.md", true},

		// Negation (last match wins).
		{"negation reinclude", []string{"*.md", "!keep.md"}, "keep.md", false},
		{"negation other excluded", []string{"*.md", "!keep.md"}, "drop.md", true},
		{"negation order", []string{"!keep.md", "*.md"}, "keep.md", true},

		// Comments and blanks are ignored.
		{"comment ignored", []string{"# a comment", "", "draft.md"}, "draft.md", true},
		{"only comment", []string{"# nothing"}, "draft.md", false},

		// Question mark.
		{"question mark", []string{"v?.md"}, "v1.md", true},
		{"question mark single", []string{"v?.md"}, "v10.md", false},

		// Character class.
		{"char class", []string{"draft-[0-9].md"}, "draft-3.md", true},
		{"char class no match", []string{"draft-[0-9].md"}, "draft-x.md", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewGitignoreMatcher(tc.patterns)
			if got := m.Match(tc.path); got != tc.want {
				t.Fatalf("Match(%q) with %v = %v, want %v", tc.path, tc.patterns, got, tc.want)
			}
		})
	}
}

func TestGitignoreMatcherEmpty(t *testing.T) {
	if m := NewGitignoreMatcher(nil); !m.Empty() {
		t.Fatal("nil patterns should be Empty")
	}
	if m := NewGitignoreMatcher([]string{"", "  ", "# c"}); !m.Empty() {
		t.Fatal("blank/comment-only patterns should be Empty")
	}
	if NewGitignoreMatcher(nil).Match("x.md") {
		t.Fatal("empty matcher must not match")
	}
}

func TestExcludedPaths(t *testing.T) {
	files := []DocFile{
		{Path: "/ws/README.md", Rel: "README.md"},
		{Path: "/ws/docs/guide.md", Rel: "docs/guide.md"},
		{Path: "/ws/archive/old.md", Rel: "archive/old.md"},
		{Path: "/ws/archive/2024/older.md", Rel: "archive/2024/older.md"},
	}
	got := ExcludedPaths(files, "/ws", []string{"archive/"})
	want := map[string]bool{
		"/ws/archive/old.md":        true,
		"/ws/archive/2024/older.md": true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %d paths", got, len(want))
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected excluded path %q", p)
		}
	}

	if ExcludedPaths(files, "/ws", nil) != nil {
		t.Fatal("no patterns should return nil")
	}
}

func TestExcludedPathsRelFallback(t *testing.T) {
	// Rel empty: the path should be computed relative to baseDir.
	files := []DocFile{{Path: "/ws/sub/x.md"}}
	got := ExcludedPaths(files, "/ws", []string{"sub/"})
	if len(got) != 1 || got[0] != "/ws/sub/x.md" {
		t.Fatalf("rel-fallback match failed: %v", got)
	}
}
