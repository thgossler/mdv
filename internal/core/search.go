package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
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

// DetectRipgrep returns the path to the ripgrep executable ("rg") if it is on
// the system PATH, or an empty string when it is not installed. Content search
// uses ripgrep when available for speed and falls back to an in-memory scan.
func DetectRipgrep() string {
	path, err := exec.LookPath("rg")
	if err != nil {
		return ""
	}
	return path
}

// SearchDocuments searches the given documents for all of the space-separated
// keywords in query, case-insensitively, combining keywords with logical AND
// per document: a document qualifies only if every keyword appears somewhere in
// it, and then every line containing any keyword is reported as a match.
//
// Results are delivered incrementally through emit, one call per qualifying
// document, so callers can stream them into a UI as they are found. emit may be
// invoked from multiple goroutines; SearchDocuments serializes those calls so
// the callback itself need not be safe for concurrent use.
//
// When rgPath is non-empty it is used as the ripgrep executable for a fast
// external scan; otherwise an in-memory scan is performed. The search honors
// ctx cancellation. A blank query emits nothing.
func SearchDocuments(ctx context.Context, files []DocFile, query, rgPath string, emit func(DocSearchResult)) {
	keywords := splitKeywords(query)
	if len(keywords) == 0 || len(files) == 0 {
		return
	}

	// Serialize emit so the callback never runs concurrently regardless of the
	// scanning strategy.
	var emitMu sync.Mutex
	safeEmit := func(r DocSearchResult) {
		emitMu.Lock()
		emit(r)
		emitMu.Unlock()
	}

	if rgPath != "" {
		if ok := searchWithRipgrep(ctx, files, keywords, rgPath, safeEmit); ok {
			return
		}
		// ripgrep failed to start or errored; fall back to the in-memory scan.
	}
	searchInMemory(ctx, files, keywords, safeEmit)
}

// splitKeywords lowercases query and splits it into distinct, non-empty
// keywords on whitespace.
func splitKeywords(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// scanLines applies the per-document AND logic to a document's raw bytes: it
// returns the matching lines (any keyword) and whether every keyword was found
// somewhere in the document. Returns ok=false if no result should be emitted.
func scanLines(content []byte, keywords []string) (matches []ContentMatch, ok bool) {
	seen := make([]bool, len(keywords))
	remaining := len(keywords)

	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Allow long lines (default bufio limit is 64 KiB).
	scanner.Buffer(make([]byte, 0, 64*1024), MaxFileBytes)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		lower := strings.ToLower(line)

		firstCol := -1
		any := false
		for ki, kw := range keywords {
			idx := strings.Index(lower, kw)
			if idx < 0 {
				continue
			}
			any = true
			if firstCol < 0 || idx < firstCol {
				firstCol = idx
			}
			if !seen[ki] {
				seen[ki] = true
				remaining--
			}
		}
		if any && len(matches) < maxMatchesPerDoc {
			text, col := excerpt(line, firstCol)
			matches = append(matches, ContentMatch{Line: lineNo, Col: col, Text: text})
		}
	}
	if remaining > 0 {
		return nil, false
	}
	return matches, true
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
// for every document that contains all keywords.
func searchInMemory(ctx context.Context, files []DocFile, keywords []string, emit func(DocSearchResult)) {
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
				if matches, ok := scanLines(content, keywords); ok {
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

// rgMessage is the subset of ripgrep's JSON output (--json) that we consume.
type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		LineNumber int `json:"line_number"`
		Lines      struct {
			Text string `json:"text"`
		} `json:"lines"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

// searchWithRipgrep runs ripgrep over the given files and emits one result per
// document that contains all keywords. It returns false if ripgrep could not be
// started or failed in a way that warrants the in-memory fallback.
func searchWithRipgrep(ctx context.Context, files []DocFile, keywords []string, rgPath string, emit func(DocSearchResult)) (ok bool) {
	if len(files) == 0 {
		return true
	}

	// Index files by path so ripgrep results can be grouped and limited to the
	// documents shown in the navigator.
	known := make(map[string]bool, len(files))
	for _, f := range files {
		known[f.Path] = true
	}

	// Multiple --regexp patterns are OR-ed by ripgrep, so each output line
	// contains at least one keyword; the per-document AND is enforced below by
	// tracking which keywords were seen before flushing a file's matches.
	args := []string{"--json", "--ignore-case", "--no-config"}
	for _, kw := range keywords {
		args = append(args, "--regexp", regexpEscape(kw))
	}
	args = append(args, "--")
	for _, f := range files {
		args = append(args, f.Path)
	}

	cmd := exec.CommandContext(ctx, rgPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}

	// Accumulate matches per document. ripgrep groups output by file (begin/
	// match.../end), so we flush when a file's "end" message arrives.
	type acc struct {
		matches   []ContentMatch
		seen      []bool
		remaining int
	}
	current := map[string]*acc{}
	getAcc := func(path string) *acc {
		a := current[path]
		if a == nil {
			a = &acc{seen: make([]bool, len(keywords)), remaining: len(keywords)}
			current[path] = a
		}
		return a
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		var msg rgMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		path := msg.Data.Path.Text
		if path != "" && !known[path] {
			continue
		}
		switch msg.Type {
		case "match":
			a := getAcc(path)
			line := strings.TrimRight(msg.Data.Lines.Text, "\r\n")
			lower := strings.ToLower(line)
			firstCol := -1
			for ki, kw := range keywords {
				idx := strings.Index(lower, kw)
				if idx < 0 {
					continue
				}
				if firstCol < 0 || idx < firstCol {
					firstCol = idx
				}
				if !a.seen[ki] {
					a.seen[ki] = true
					a.remaining--
				}
			}
			if firstCol < 0 {
				firstCol = 0
			}
			if len(a.matches) < maxMatchesPerDoc {
				text, col := excerpt(line, firstCol)
				a.matches = append(a.matches, ContentMatch{Line: msg.Data.LineNumber, Col: col, Text: text})
			}
		case "end":
			a := current[path]
			delete(current, path)
			if a != nil && a.remaining == 0 {
				emit(DocSearchResult{Path: path, Matches: a.matches})
			}
		}
	}

	// Drain the command. ripgrep exits 1 when there are no matches, which is not
	// an error for our purposes.
	_ = cmd.Wait()
	return true
}

// regexpEscape escapes regex metacharacters so a keyword is matched literally by
// ripgrep.
func regexpEscape(s string) string {
	const special = `\.+*?()|[]{}^$`
	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
