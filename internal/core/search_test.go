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
	SearchDocuments(context.Background(), files, query, func(r DocSearchResult) {
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

func TestSearchDocuments_PhraseSameLine(t *testing.T) {
	dir := t.TempDir()
	// a.md has both query words together on one line -> matches.
	a := writeDoc(t, dir, "a.md", "the alpha beta line\ngamma there\n")
	c := writeDoc(t, dir, "c.md", "nothing relevant\n")

	res := collect(t, []DocFile{a, c}, "alpha beta")

	if _, ok := res["a.md"]; !ok {
		t.Errorf("a.md should qualify (alpha beta on one line)")
	}
	if _, ok := res["c.md"]; ok {
		t.Errorf("c.md should NOT qualify")
	}
}

func TestSearchDocuments_PhraseAcrossLineBreak(t *testing.T) {
	dir := t.TempDir()
	// Hard-wrapped text: "alpha" ends one line and "beta" begins the next, so
	// the multi-word phrase is split across a line break but must still match,
	// reported on the line where it begins.
	wrap := writeDoc(t, dir, "wrap.md", "lorem ipsum dolor alpha\nbeta sit amet consectetur\n")
	// Words separated by a blank line (a paragraph boundary) are two lines apart
	// and must NOT match.
	para := writeDoc(t, dir, "para.md", "something alpha\n\nbeta something\n")

	res := collect(t, []DocFile{wrap, para}, "alpha beta")

	r, ok := res["wrap.md"]
	if !ok {
		t.Fatalf("wrap.md should match a phrase wrapped across a line break")
	}
	if len(r.Matches) != 1 || r.Matches[0].Line != 1 {
		t.Errorf("expected one match starting on line 1, got %+v", r.Matches)
	}
	if _, ok := res["para.md"]; ok {
		t.Errorf("para.md should NOT match across a blank-line paragraph boundary")
	}
}

func TestSearchDocuments_FuzzyPhrase(t *testing.T) {
	dir := t.TempDir()
	// "client approvals" must match "Client-side Approvals": the filler token
	// "side" sits between the two matched words, and "approvals" is matched by
	// the query word "approvals".
	a := writeDoc(t, dir, "a.md", "# Client-side Approvals\nbody text\n")

	res := collect(t, []DocFile{a}, "client approvals")
	r, ok := res["a.md"]
	if !ok {
		t.Fatalf("a.md should match the fuzzy phrase")
	}
	if len(r.Matches) != 1 || r.Matches[0].Line != 1 {
		t.Errorf("expected one match on line 1, got %+v", r.Matches)
	}
}

func TestSearchDocuments_TypoTolerance(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.md", "Approval workflow documentation\n")

	// One transposed/dropped letter still matches within the edit-distance budget.
	res := collect(t, []DocFile{a}, "aproval")
	if _, ok := res["a.md"]; !ok {
		t.Errorf("a.md should match despite the typo 'aproval'")
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

func TestSearchDocuments_LinesMatchingPhrase(t *testing.T) {
	dir := t.TempDir()
	// The phrase "alpha beta" appears on lines 1 and 3; line 2 has neither in
	// sequence, so only the two phrase lines are reported.
	a := writeDoc(t, dir, "a.md", "first alpha beta line\nmiddle nothing\nlast alpha beta line\n")

	res := collect(t, []DocFile{a}, "alpha beta")
	r := res["a.md"]
	if len(r.Matches) != 2 {
		t.Fatalf("expected 2 phrase match lines, got %d: %+v", len(r.Matches), r.Matches)
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

func TestTokenize(t *testing.T) {
	toks := tokenize("Client-side Approvals!")
	got := make([]string, len(toks))
	for i, tk := range toks {
		got[i] = tk.text
	}
	want := []string{"client", "side", "approvals"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if tokenize("   ") != nil {
		t.Errorf("blank string should yield no tokens")
	}
}

func TestWordMatch(t *testing.T) {
	cases := []struct {
		q, tok string
		want   bool
	}{
		{"client", "client", true},      // exact
		{"approval", "approvals", true}, // substring (prefix)
		{"approvals", "approval", true}, // one edit away (drop trailing 's')
		{"aproval", "approval", true},   // single edit (typo)
		{"cat", "dog", false},           // short word, no substring -> no fuzzy
		{"", "anything", false},         // empty query word never matches
	}
	for _, c := range cases {
		if got := wordMatch(c.q, c.tok); got != c.want {
			t.Errorf("wordMatch(%q, %q) = %v, want %v", c.q, c.tok, got, c.want)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		hay, q string
		want   bool
	}{
		{"Client-side Approvals.md", "client approvals", true}, // fuzzy phrase over a filename
		{"Quarterly Report 2024", "quarterly report", true},    // exact words in order
		{"Budget Overview", "client approvals", false},         // unrelated
		{"Approval Workflow", "aproval", true},                 // typo tolerance
		{"anything at all", "", true},                          // blank query matches anything
		{"", "client", false},                                  // empty haystack never matches a query
	}
	for _, c := range cases {
		if got := FuzzyMatch(c.hay, c.q); got != c.want {
			t.Errorf("FuzzyMatch(%q, %q) = %v, want %v", c.hay, c.q, got, c.want)
		}
	}
}

func TestMatchPhraseSpans(t *testing.T) {
	cases := []struct {
		name, s, q string
		want       []PhraseSpan
	}{
		{
			name: "hyphenated phrase",
			s:    "azdw mcp --client-approvals",
			q:    "client approvals",
			want: []PhraseSpan{{Start: 11, End: 27}}, // "client-approvals"
		},
		{
			name: "phrase with filler word",
			s:    "the Client-side Approvals dialog",
			q:    "client approvals",
			want: []PhraseSpan{{Start: 4, End: 25}}, // "Client-side Approvals"
		},
		{
			name: "single word spans whole token",
			s:    "approve the approvals now",
			q:    "approvals",
			want: []PhraseSpan{{Start: 12, End: 21}}, // "approvals"
		},
		{
			name: "no match",
			s:    "budget overview only",
			q:    "client approvals",
			want: nil,
		},
		{
			name: "blank query",
			s:    "anything",
			q:    "",
			want: nil,
		},
	}
	for _, c := range cases {
		got := MatchPhraseSpans(c.s, c.q)
		if len(got) != len(c.want) {
			t.Fatalf("%s: MatchPhraseSpans(%q, %q) = %v, want %v", c.name, c.s, c.q, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: span %d = %+v, want %+v (matched %q)", c.name, i, got[i], c.want[i], c.s[got[i].Start:got[i].End])
			}
		}
	}
}
