package tui

import (
	"strings"
	"testing"
)

func TestHighlightTermPlain(t *testing.T) {
	const on = "\x1b[30;43m"
	const off = "\x1b[39;49m"

	got := highlightTerm("the quick brown fox", "quick", -1, -1)
	want := "the " + on + "quick" + off + " brown fox"
	if got != want {
		t.Errorf("highlightTerm plain:\n got %q\nwant %q", got, want)
	}
}

func TestHighlightTermCurrentLineGreen(t *testing.T) {
	// Two match lines; only the current one (line 1, col 0) should be green.
	got := highlightTerm("foo\nfoo", "foo", 1, 0)
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "\x1b[30;43m") {
		t.Errorf("non-current line should be yellow: %q", lines[0])
	}
	if !strings.Contains(lines[1], "\x1b[30;42m") {
		t.Errorf("current line should be green: %q", lines[1])
	}
}

func TestHighlightTermIndependentOccurrencesOnSameLine(t *testing.T) {
	// Three "foo" on one line; only the second (col 4) is the current match,
	// so exactly one green and two yellow highlights are expected.
	got := highlightTerm("foo foo foo", "foo", 0, 4)
	if n := strings.Count(got, "\x1b[30;42m"); n != 1 {
		t.Errorf("expected exactly 1 green highlight, got %d in %q", n, got)
	}
	if n := strings.Count(got, "\x1b[30;43m"); n != 2 {
		t.Errorf("expected exactly 2 yellow highlights, got %d in %q", n, got)
	}
	if stripANSI(got) != "foo foo foo" {
		t.Errorf("visible text changed: %q", stripANSI(got))
	}
}

func TestHighlightTermCaseInsensitiveMultiple(t *testing.T) {
	got := highlightTerm("Foo foo FOO", "foo", -1, -1)
	// All three occurrences must be wrapped, and stripping ANSI must restore
	// the original visible text exactly.
	if n := strings.Count(got, "\x1b[30;43m"); n != 3 {
		t.Errorf("expected 3 highlights, got %d in %q", n, got)
	}
	if stripANSI(got) != "Foo foo FOO" {
		t.Errorf("visible text changed: %q", stripANSI(got))
	}
}

func TestHighlightTermPreservesAnsiAndVisibleText(t *testing.T) {
	// A styled line: "bold" is wrapped in red. Searching for "old" must keep
	// the visible characters intact when ANSI is stripped.
	styled := "a \x1b[31mbold\x1b[0m word"
	got := highlightTerm(styled, "old", -1, -1)
	if stripANSI(got) != "a bold word" {
		t.Errorf("visible text changed: %q", stripANSI(got))
	}
	if !strings.Contains(got, "\x1b[30;43m") {
		t.Errorf("match was not highlighted: %q", got)
	}
}

func TestHighlightTermNoMatch(t *testing.T) {
	in := "nothing to see here"
	if got := highlightTerm(in, "xyz", -1, -1); got != in {
		t.Errorf("no-match input was modified: %q", got)
	}
}

func TestHighlightTermEmpty(t *testing.T) {
	in := "unchanged"
	if got := highlightTerm(in, "", -1, -1); got != in {
		t.Errorf("empty term modified input: %q", got)
	}
}
