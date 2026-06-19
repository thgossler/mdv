package core

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"sync"
	"unicode"
)

// ContentMatch is a single matching line within a document. Line is 1-based and
// refers to the raw markdown source so callers can jump to it. Text is a short,
// trimmed excerpt of the line for display.
type ContentMatch struct {
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

// DocSearchResult groups all content matches found in one document.
type DocSearchResult struct {
	Path    string         `json:"path"`
	Matches []ContentMatch `json:"matches"`
}

// maxMatchesPerDoc caps how many match lines are reported per document so a
// keyword that appears thousands of times does not flood the navigator.
const maxMatchesPerDoc = 200

// matchContextChars is the maximum length of a displayed match excerpt. Longer
// lines are trimmed around the first keyword occurrence with ellipses.
const matchContextChars = 120

// maxPhraseGap is how many non-matching word tokens may sit between two
// consecutive query words and still count as part of the same phrase. It lets a
// query like "client approvals" match "Client-side Approvals" (one filler token,
// "side", between the two matched words).
const maxPhraseGap = 2

// maxLineSpan is how many lines apart two consecutive matched query words may be
// and still count as the same phrase. A value of 1 lets a phrase span a single
// line break - the common case when markdown source is hard-wrapped at ~80
// columns - while still rejecting words separated by a blank line (a paragraph
// boundary), which would be two lines apart.
const maxLineSpan = 1

// SearchDocuments searches the given documents for query as a smart, fuzzy
// phrase: the query's words must appear in order and close together (allowing a
// few intervening words and minor spelling differences), rather than each word
// matching independently anywhere in the document. The phrase may span a single
// line break, so a term hard-wrapped across two lines still matches. For example
// "client approvals" matches "Client-side Approvals". Matching is
// case-insensitive and operates on whole word tokens, so a query word matches a
// longer token it is contained in (e.g. "approval" matches "approvals") or one
// within a small edit distance (typo tolerance).
//
// A document qualifies when at least one phrase match is found, and the line on
// which each match begins is reported.
//
// Results are delivered incrementally through emit, one call per qualifying
// document, so callers can stream them into a UI as they are found. emit may be
// invoked from multiple goroutines; SearchDocuments serializes those calls so
// the callback itself need not be safe for concurrent use.
//
// The search is performed entirely in memory and honors ctx cancellation. A
// blank query emits nothing.
func SearchDocuments(ctx context.Context, files []DocFile, query string, emit func(DocSearchResult)) {
	words := queryWords(query)
	if len(words) == 0 || len(files) == 0 {
		return
	}

	// Serialize emit so the callback never runs concurrently.
	var emitMu sync.Mutex
	safeEmit := func(r DocSearchResult) {
		emitMu.Lock()
		emit(r)
		emitMu.Unlock()
	}

	searchInMemory(ctx, files, words, safeEmit)
}

// queryWords lowercases query and splits it into its ordered word tokens,
// preserving duplicates so the phrase is matched faithfully. A word token is a
// maximal run of letters and digits.
func queryWords(query string) []string {
	toks := tokenize(query)
	if len(toks) == 0 {
		return nil
	}
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.text
	}
	return out
}

// FuzzyMatch reports whether query matches haystack as a smart fuzzy phrase,
// using the same matching rules as the document content search: the query's
// words must appear in order and close together within haystack, tolerating a
// few intervening words, minor spelling differences and a query word contained
// in a longer one. A blank query matches anything. This powers the navigator's
// filename/title filtering so it behaves consistently with content search.
func FuzzyMatch(haystack, query string) bool {
	words := queryWords(query)
	if len(words) == 0 {
		return true
	}
	_, ok := matchPhrase(tokenize(haystack), words)
	return ok
}

// token is a single word occurrence within a string: its lowercased text and
// the byte offset where it begins in the original string.
type token struct {
	text  string
	start int
}

// isWordRune reports whether r is part of a word token (letters and digits).
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// tokenize splits s into lowercase word tokens with their byte offsets. Tokens
// are maximal runs of letters and digits; every other rune is a separator, so
// "Client-side" yields the two tokens "client" and "side".
func tokenize(s string) []token {
	var toks []token
	start := -1
	for i, r := range s {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			toks = append(toks, token{text: strings.ToLower(s[start:i]), start: start})
			start = -1
		}
	}
	if start >= 0 {
		toks = append(toks, token{text: strings.ToLower(s[start:]), start: start})
	}
	return toks
}

// lineToken is a word token tagged with the 0-based index of the line it occurs
// on, so a phrase match can be allowed to span a line break while still knowing
// where (and on which line) it begins.
type lineToken struct {
	text  string
	start int // byte offset within its line
	line  int
}

// splitLines splits a document's raw bytes into its individual lines (without
// trailing newlines), preserving the original text for excerpting.
func splitLines(content []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Allow long lines (default bufio limit is 64 KiB).
	scanner.Buffer(make([]byte, 0, 64*1024), MaxFileBytes)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// streamTokens flattens the document's lines into a single ordered token stream,
// tagging each token with its line index so phrase matching can cross line
// breaks.
func streamTokens(lines []string) []lineToken {
	var toks []lineToken
	for li, line := range lines {
		for _, t := range tokenize(line) {
			toks = append(toks, lineToken{text: t.text, start: t.start, line: li})
		}
	}
	return toks
}

// scanLines applies the fuzzy-phrase logic to a document's raw bytes. The phrase
// may span a line break (up to maxLineSpan lines between consecutive matched
// words), which catches multi-word terms hard-wrapped across two source lines.
// It reports the starting line of each match (at most one per starting line) and
// whether any match was found.
func scanLines(content []byte, words []string) (matches []ContentMatch, ok bool) {
	lines := splitLines(content)
	toks := streamTokens(lines)

	seenLine := make(map[int]bool)
	for s := 0; s < len(toks); s++ {
		if seenLine[toks[s].line] {
			continue
		}
		if !wordMatch(words[0], toks[s].text) {
			continue
		}
		if !alignPhrase(toks, s, words) {
			continue
		}
		li := toks[s].line
		seenLine[li] = true
		text, c := excerpt(lines[li], toks[s].start)
		matches = append(matches, ContentMatch{Line: li + 1, Col: c, Text: text})
		if len(matches) >= maxMatchesPerDoc {
			break
		}
	}
	if len(matches) == 0 {
		return nil, false
	}
	return matches, true
}

// alignPhrase reports whether the ordered query words match the token stream
// starting with words[0] anchored at toks[s]. Each subsequent word must
// fuzzy-match a later token within maxPhraseGap non-matching tokens and within
// maxLineSpan lines of the previously matched word, so a phrase can cross one
// line break but not a blank-line paragraph boundary.
func alignPhrase(toks []lineToken, s int, words []string) bool {
	ti := s + 1
	prevLine := toks[s].line
	for qi := 1; qi < len(words); qi++ {
		found := false
		for gap := 0; ti < len(toks) && gap <= maxPhraseGap; gap++ {
			cand := toks[ti]
			if cand.line-prevLine > maxLineSpan {
				return false // next word is too far down; the phrase is broken
			}
			if wordMatch(words[qi], cand.text) {
				prevLine = cand.line
				ti++
				found = true
				break
			}
			ti++
		}
		if !found {
			return false
		}
	}
	return true
}

// matchPhrase reports whether the ordered query words appear as a fuzzy phrase
// within the line's tokens. Each query word must fuzzy-match a token, the
// matched tokens must occur in order, and at most maxPhraseGap non-matching
// tokens may separate two consecutive matches. It returns the byte offset of the
// first matched token (for excerpting) and whether a match was found.
func matchPhrase(tokens []token, words []string) (col int, ok bool) {
	if len(words) == 0 || len(tokens) == 0 {
		return 0, false
	}
	n := len(tokens)
	for s := 0; s < n; s++ {
		if !wordMatch(words[0], tokens[s].text) {
			continue
		}
		// words[0] anchored at s; align the rest allowing small gaps.
		ti := s + 1
		matched := true
		for qi := 1; qi < len(words); qi++ {
			found := false
			for gap := 0; ti < n && gap <= maxPhraseGap; gap++ {
				if wordMatch(words[qi], tokens[ti].text) {
					ti++
					found = true
					break
				}
				ti++
			}
			if !found {
				matched = false
				break
			}
		}
		if matched {
			return tokens[s].start, true
		}
	}
	return 0, false
}

// PhraseSpan is the byte range [Start, End) of a fuzzy-phrase match within a
// single string. It spans from the start of the first matched word to the end of
// the last matched word, so the whole matched phrase can be highlighted.
type PhraseSpan struct {
	Start int
	End   int
}

// MatchPhraseSpans returns the byte ranges of every non-overlapping fuzzy-phrase
// match of query within s, in order of appearance, using the same matching rules
// as the document content search (words in order, small gaps, substring and typo
// tolerance). This lets callers highlight in-document matches consistently with
// content search - for example "client approvals" matches the phrase in
// "azdw mcp --client-approvals". A blank query yields no spans.
func MatchPhraseSpans(s, query string) []PhraseSpan {
	words := queryWords(query)
	if len(words) == 0 {
		return nil
	}
	toks := tokenize(s)
	var spans []PhraseSpan
	for i := 0; i < len(toks); {
		if !wordMatch(words[0], toks[i].text) {
			i++
			continue
		}
		end, ok := alignPhraseFrom(toks, i, words)
		if !ok {
			i++
			continue
		}
		last := toks[end]
		spans = append(spans, PhraseSpan{Start: toks[i].start, End: last.start + len(last.text)})
		i = end + 1
	}
	return spans
}

// alignPhraseFrom reports whether the ordered query words match the token slice
// starting with words[0] anchored at toks[s], returning the index of the last
// matched token. It mirrors matchPhrase's alignment, allowing up to maxPhraseGap
// non-matching tokens between consecutive matched words.
func alignPhraseFrom(toks []token, s int, words []string) (end int, ok bool) {
	ti := s + 1
	last := s
	for qi := 1; qi < len(words); qi++ {
		found := false
		for gap := 0; ti < len(toks) && gap <= maxPhraseGap; gap++ {
			if wordMatch(words[qi], toks[ti].text) {
				last = ti
				ti++
				found = true
				break
			}
			ti++
		}
		if !found {
			return 0, false
		}
	}
	return last, true
}

// wordMatch reports whether query word q matches token t: as a substring
// (covering exact, prefix and infix matches such as "approval" in "approvals")
// or within a small Levenshtein edit distance for typo tolerance. The edit
// distance budget grows with q's length, and very short words must match as a
// substring to avoid spurious fuzzy hits.
func wordMatch(q, t string) bool {
	if q == "" {
		return false
	}
	if strings.Contains(t, q) {
		return true
	}
	qr := []rune(q)
	d := maxEditDist(len(qr))
	if d == 0 {
		return false
	}
	tr := []rune(t)
	if abs(len(qr)-len(tr)) > d {
		return false
	}
	return levenshtein(qr, tr) <= d
}

// maxEditDist returns the Levenshtein budget for a query word of n runes.
func maxEditDist(n int) int {
	switch {
	case n >= 8:
		return 2
	case n >= 5:
		return 1
	default:
		return 0
	}
}

// levenshtein computes the edit distance between two rune slices.
func levenshtein(a, b []rune) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// excerpt trims a long line to matchContextChars characters, keeping a window
// around the keyword at byteCol. It returns the (possibly shortened) display
// text and the column of the keyword within that text.
func excerpt(line string, byteCol int) (string, int) {
	runes := []rune(line)
	// Convert the byte column to a rune index.
	runeCol := len([]rune(line[:clamp(byteCol, 0, len(line))]))

	// Trim leading whitespace for display, tracking the shift.
	lead := 0
	for lead < len(runes) && (runes[lead] == ' ' || runes[lead] == '\t') {
		lead++
	}
	if lead > 0 && runeCol >= lead {
		runes = runes[lead:]
		runeCol -= lead
	} else if lead > 0 {
		runes = runes[lead:]
		runeCol = 0
	}

	if len(runes) <= matchContextChars {
		return string(runes), runeCol
	}

	// Center the window on the keyword.
	half := matchContextChars / 2
	start := runeCol - half
	if start < 0 {
		start = 0
	}
	end := start + matchContextChars
	if end > len(runes) {
		end = len(runes)
		start = end - matchContextChars
		if start < 0 {
			start = 0
		}
	}
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	suffix := ""
	if end < len(runes) {
		suffix = "…"
	}
	text := prefix + string(runes[start:end]) + suffix
	col := runeCol - start + len([]rune(prefix))
	return text, col
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// searchInMemory scans each document's bytes concurrently and emits a result
// for every document with at least one line matching the query phrase.
func searchInMemory(ctx context.Context, files []DocFile, words []string, emit func(DocSearchResult)) {
	const workers = 8
	jobs := make(chan DocFile)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if ctx.Err() != nil {
					return
				}
				content, err := ReadMarkdownFile(f.Path)
				if err != nil {
					continue // skip oversized/unreadable files
				}
				if matches, ok := scanLines(content, words); ok {
					emit(DocSearchResult{Path: f.Path, Matches: matches})
				}
			}
		}()
	}

	// Feed jobs, but stop promptly when the context is cancelled. Selecting on
	// ctx.Done() during the send guarantees the sender never blocks forever even
	// if every worker has already exited on cancellation.
	for _, f := range files {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- f:
		}
	}
	close(jobs)
	wg.Wait()
}
