package core

import (
	"path/filepath"
	"regexp"
	"strings"
)

// GitignoreMatcher matches relative paths against an ordered list of
// .gitignore-style patterns. The last pattern that matches a path decides the
// outcome, so a later negation ("!pattern") can re-include a path that an
// earlier rule excluded.
//
// The supported subset covers the patterns that are useful for filtering a
// document navigator:
//   - blank lines and lines starting with '#' are ignored;
//   - a leading '!' negates (re-includes) a match;
//   - a trailing '/' restricts the pattern to directories (and their contents);
//   - a leading '/' (or any '/' in the middle) anchors the pattern to the
//     workspace root; an otherwise slash-free pattern matches at any depth;
//   - '*' matches any run of characters except '/', '?' matches a single such
//     character, '**' spans directory boundaries, and '[...]' character classes
//     are passed through to the regexp engine.
type GitignoreMatcher struct {
	rules []gitignoreRule
}

type gitignoreRule struct {
	re     *regexp.Regexp
	negate bool
}

// NewGitignoreMatcher compiles the given patterns (one per slice element).
// Invalid or empty patterns are skipped. The returned matcher is safe to reuse.
func NewGitignoreMatcher(patterns []string) *GitignoreMatcher {
	m := &GitignoreMatcher{}
	for _, raw := range patterns {
		line := strings.TrimRight(raw, " \t\r\n")
		line = strings.TrimLeft(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = line[1:]
		}
		// An escaped leading '#' or '!' is a literal first character.
		if strings.HasPrefix(line, "\\#") || strings.HasPrefix(line, "\\!") {
			line = line[1:]
		}

		dirOnly := false
		if strings.HasSuffix(line, "/") {
			dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}

		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		// A separator anywhere in the remaining pattern also anchors it to root.
		if strings.Contains(line, "/") {
			anchored = true
		}

		re := compileGitignore(line, anchored, dirOnly)
		if re == nil {
			continue
		}
		m.rules = append(m.rules, gitignoreRule{re: re, negate: negate})
	}
	return m
}

// Empty reports whether the matcher has no usable rules.
func (m *GitignoreMatcher) Empty() bool {
	return m == nil || len(m.rules) == 0
}

// Match reports whether the given relative path (slash- or OS-separated) is
// ignored by the compiled patterns.
func (m *GitignoreMatcher) Match(relPath string) bool {
	if m.Empty() {
		return false
	}
	p := strings.TrimPrefix(filepath.ToSlash(relPath), "./")
	ignored := false
	for _, r := range m.rules {
		if r.re.MatchString(p) {
			ignored = !r.negate
		}
	}
	return ignored
}

// compileGitignore converts a single (already pre-processed) gitignore pattern
// into an anchored regular expression matching slash-separated relative paths.
// A nil return means the pattern could not be compiled and should be skipped.
func compileGitignore(pat string, anchored, dirOnly bool) *regexp.Regexp {
	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		// Non-anchored patterns match at any directory depth.
		b.WriteString("^(?:.*/)?")
	}

	n := len(pat)
	for i := 0; i < n; {
		c := pat[i]
		switch c {
		case '*':
			// Collapse a run of '*' and decide between '*' and '**'.
			j := i
			for j < n && pat[j] == '*' {
				j++
			}
			if j-i >= 2 {
				beforeSlash := i == 0 || pat[i-1] == '/'
				afterSlash := j < n && pat[j] == '/'
				if beforeSlash && afterSlash {
					// "**/" spans zero or more leading directories.
					b.WriteString("(?:.*/)?")
					i = j + 1 // consume the trailing slash too
					continue
				}
				// Any other "**" spans across directory boundaries.
				b.WriteString(".*")
				i = j
				continue
			}
			// Single '*' stays within a path segment.
			b.WriteString("[^/]*")
			i++
		case '?':
			b.WriteString("[^/]")
			i++
		case '[':
			// Pass a character class through, converting a leading '!' negation
			// to the regexp '^' form.
			j := i + 1
			if j < n && (pat[j] == '!' || pat[j] == '^') {
				j++
			}
			if j < n && pat[j] == ']' {
				j++
			}
			for j < n && pat[j] != ']' {
				j++
			}
			if j >= n {
				// Unterminated class: treat '[' as a literal.
				b.WriteString("\\[")
				i++
				continue
			}
			class := pat[i : j+1]
			if strings.HasPrefix(class, "[!") {
				class = "[^" + class[2:]
			}
			b.WriteString(class)
			i = j + 1
		default:
			if isRegexMeta(c) {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
			i++
		}
	}

	if dirOnly {
		// Directory patterns match the directory's contents (our candidates are
		// files, which always live below the directory).
		b.WriteString("/.*$")
	} else {
		// Match the path itself or anything nested below it.
		b.WriteString("(?:/.*)?$")
	}

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// isRegexMeta reports whether c is a regexp metacharacter that must be escaped
// when it appears literally in a gitignore pattern. '/' is intentionally not
// included (it is a literal in both syntaxes), and the glob metacharacters
// '*', '?' and '[' are handled separately by the caller.
func isRegexMeta(c byte) bool {
	switch c {
	case '.', '+', '(', ')', '{', '}', '$', '^', '|', '\\':
		return true
	}
	return false
}

// ExcludedPaths returns the absolute paths of the documents whose
// workspace-relative path is ignored by the given patterns. Documents are
// matched on their Rel field when present, falling back to a path computed
// relative to baseDir. It returns nil when there are no usable patterns.
func ExcludedPaths(files []DocFile, baseDir string, patterns []string) []string {
	m := NewGitignoreMatcher(patterns)
	if m.Empty() {
		return nil
	}
	var out []string
	for _, f := range files {
		rel := f.Rel
		if rel == "" {
			if r, err := filepath.Rel(baseDir, f.Path); err == nil {
				rel = filepath.ToSlash(r)
			}
		}
		if rel == "" {
			continue
		}
		if m.Match(rel) {
			out = append(out, f.Path)
		}
	}
	return out
}
