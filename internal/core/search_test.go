package core

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// collect runs SearchDocuments with the in-memory engine and returns results
// keyed by base filename for easy assertions.
func collect(t *testing.T, files []DocFile, query string) map[string]DocSearchResult {
	t.Helper()
	out := map[string]DocSearchResult{}
	var mu sync.Mutex
	SearchDocuments(context.Background(), files, query, "", func(r DocSearchResult) {
		mu.Lock()
		out[filepath.Base(r.Path)] = r
		mu.Unlock()
	})
	return out
}

func writeDoc(t *testing.T, dir, name, content string) DocFile {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return DocFile{Path: p, Name: name}
}

func TestSearchDocuments_AndPerDocument(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.md", "alpha here\nbeta there\n")
	b := writeDoc(t, dir, "b.md", "alpha only\ngamma line\n")
	c := writeDoc(t, dir, "c.md", "nothing relevant\n")

	res := collect(t, []DocFile{a, b, c}, "alpha beta")

	if _, ok := res["a.md"]; !ok {
		t.Errorf("a.md should qualify (has alpha and beta)")
	}
	if _, ok := res["b.md"]; ok {
		t.Errorf("b.md should NOT qualify (missing beta)")
	}
	if _, ok := res["c.md"]; ok {
		t.Errorf("c.md should NOT qualify")
	}
}

func TestSearchDocuments_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.md", "The Quick Brown Fox\n")

	res := collect(t, []DocFile{a}, "quick FOX")
	r, ok := res["a.md"]
	if !ok {
		t.Fatalf("a.md should match case-insensitively")
	}
	if len(r.Matches) != 1 || r.Matches[0].Line != 1 {
		t.Errorf("expected one match on line 1, got %+v", r.Matches)
	}
}

func TestSearchDocuments_LinesMatchingAnyKeyword(t *testing.T) {
	dir := t.TempDir()
	// Doc qualifies via both keywords; each line has only one keyword.
	a := writeDoc(t, dir, "a.md", "first alpha line\nmiddle nothing\nlast beta line\n")

	res := collect(t, []DocFile{a}, "alpha beta")
	r := res["a.md"]
	if len(r.Matches) != 2 {
		t.Fatalf("expected 2 match lines (any keyword), got %d: %+v", len(r.Matches), r.Matches)
	}
	lines := []int{r.Matches[0].Line, r.Matches[1].Line}
	sort.Ints(lines)
	if lines[0] != 1 || lines[1] != 3 {
		t.Errorf("expected matches on lines 1 and 3, got %v", lines)
	}
}

func TestSearchDocuments_SkipsNonListedFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.md", "alpha\n")
	// b exists on disk but is NOT in the files slice; must never be searched.
	writeDoc(t, dir, "b.md", "alpha\n")

	res := collect(t, []DocFile{a}, "alpha")
	if _, ok := res["b.md"]; ok {
		t.Errorf("b.md was not in the file list and must not appear")
	}
	if _, ok := res["a.md"]; !ok {
		t.Errorf("a.md should match")
	}
}

func TestSearchDocuments_BlankQuery(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.md", "alpha\n")
	res := collect(t, []DocFile{a}, "   ")
	if len(res) != 0 {
		t.Errorf("blank query should emit nothing, got %d", len(res))
	}
}

func TestSearchDocuments_ContextTruncation(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x ", 200) + "needle " + strings.Repeat("y ", 200)
	a := writeDoc(t, dir, "a.md", long+"\n")

	res := collect(t, []DocFile{a}, "needle")
	r := res["a.md"]
	if len(r.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(r.Matches))
	}
	text := r.Matches[0].Text
	if len([]rune(text)) > matchContextChars+2 { // +2 for ellipses
		t.Errorf("excerpt not truncated: %d runes", len([]rune(text)))
	}
	if !strings.Contains(strings.ToLower(text), "needle") {
		t.Errorf("excerpt should contain the keyword: %q", text)
	}
	// The keyword column should point at the keyword within the excerpt.
	runes := []rune(text)
	col := r.Matches[0].Col
	if col < 0 || col+len("needle") > len(runes) ||
		strings.ToLower(string(runes[col:col+len("needle")])) != "needle" {
		t.Errorf("col %d does not point at the keyword in %q", col, text)
	}
}

func TestSearchDocuments_RipgrepParity(t *testing.T) {
	rg := DetectRipgrep()
	if rg == "" {
		t.Skip("ripgrep (rg) not installed; skipping parity test")
	}
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.md", "first alpha line\nmiddle nothing\nlast beta line\n")
	b := writeDoc(t, dir, "b.md", "alpha only\ngamma line\n")
	files := []DocFile{a, b}

	rgRes := map[string]DocSearchResult{}
	var mu sync.Mutex
	SearchDocuments(context.Background(), files, "alpha beta", rg, func(r DocSearchResult) {
		mu.Lock()
		rgRes[filepath.Base(r.Path)] = r
		mu.Unlock()
	})

	if _, ok := rgRes["a.md"]; !ok {
		t.Errorf("rg: a.md should qualify (alpha and beta)")
	}
	if _, ok := rgRes["b.md"]; ok {
		t.Errorf("rg: b.md should NOT qualify (missing beta)")
	}
	if r := rgRes["a.md"]; len(r.Matches) != 2 {
		t.Errorf("rg: a.md expected 2 match lines, got %d: %+v", len(r.Matches), r.Matches)
	}
}

func TestSplitKeywords(t *testing.T) {
	got := splitKeywords("  Foo   bar foo BAR baz ")
	want := []string{"foo", "bar", "baz"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("keyword[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if splitKeywords("   ") != nil {
		t.Errorf("blank query should yield nil keywords")
	}
}
