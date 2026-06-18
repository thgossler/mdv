package mdfmt

import (
	"regexp"
	"testing"
)

func TestUnescapeEntities(t *testing.T) {
	cases := map[string]string{
		"a &amp; b":               "a & b",
		"&lt;tag&gt;":             "<tag>",
		"&quot;quoted&quot;":      "\"quoted\"",
		"it&#39;s &apos;ok&apos;": "it's 'ok'",
		"non&nbsp;break":          "non break",
		"no entities here":        "no entities here",
		"&amp;amp;":               "&amp;", // single pass: outer &amp; -> &
	}
	for in, want := range cases {
		if got := unescapeEntities(in); got != want {
			t.Errorf("unescapeEntities(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeindent(t *testing.T) {
	cases := map[string]string{
		"    indented":     "indented",
		"\t\ttabbed":       "tabbed",
		"line1\n    line2": "line1\nline2",
		"  a\n\tb\n   c":   "a\nb\nc",
		"no-indent":        "no-indent",
		"":                 "",
	}
	for in, want := range cases {
		if got := deindent(in); got != want {
			t.Errorf("deindent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsRuleRow(t *testing.T) {
	truthy := []string{
		"-------",
		"│ --- │ --- │",
		"┼─────┼─────┼",
		"+-----+-----+",
		"--- | ---",
	}
	for _, s := range truthy {
		if !isRuleRow(s) {
			t.Errorf("isRuleRow(%q) = false, want true", s)
		}
	}
	falsy := []string{
		"",
		"   ",
		"-- ", // only 2 dashes, below threshold
		"normal text",
		"| Header | Other |",
		"a-b-c", // contains letters
	}
	for _, s := range falsy {
		if isRuleRow(s) {
			t.Errorf("isRuleRow(%q) = true, want false", s)
		}
	}
}

func TestIsTableLine(t *testing.T) {
	truthy := []string{
		"│ a │ b │",
		"| a | b |",
		"┼─────┼", // rule row glyphs
		"------",  // ascii rule
	}
	for _, s := range truthy {
		if !isTableLine(s) {
			t.Errorf("isTableLine(%q) = false, want true", s)
		}
	}
	falsy := []string{
		"plain prose line",
		"",
	}
	for _, s := range falsy {
		if isTableLine(s) {
			t.Errorf("isTableLine(%q) = true, want false", s)
		}
	}
}

func TestCleanURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com":   "https://example.com",
		"  spaced  ":            "spaced",
		"<https://example.com>": "https://example.com",
		"url \"a title\"":       "url",          // inline title dropped at first space
		"url\twith-tab":         "url",          // tab also delimits
		"<unterminated":         "unterminated", // lone '<' trimmed
	}
	for in, want := range cases {
		if got := cleanURL(in); got != want {
			t.Errorf("cleanURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMdImgWidth(t *testing.T) {
	cases := map[string]int{
		"![a](img.png =800x600)": 800,
		"![a](img.png =500)":     500,
		"![a](img.png =x600)":    0, // height-only -> width 0
		"![a](img.png)":          0, // no size hint
		"plain text":             0,
	}
	for in, want := range cases {
		if got := mdImgWidth(in); got != want {
			t.Errorf("mdImgWidth(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFirstGroupAndFirstInt(t *testing.T) {
	if got := firstGroup(reSrcAttr, `<img src="logo.png">`); got != "logo.png" {
		t.Errorf("firstGroup src = %q, want logo.png", got)
	}
	if got := firstGroup(reSrcAttr, `<img alt="no src">`); got != "" {
		t.Errorf("firstGroup no-src = %q, want empty", got)
	}

	reNum := regexp.MustCompile(`width="([^"]*)"`)
	if got := firstInt(reNum, `width="640"`); got != 640 {
		t.Errorf("firstInt = %d, want 640", got)
	}
	if got := firstInt(reNum, `width="auto"`); got != 0 {
		t.Errorf("firstInt non-digit = %d, want 0", got)
	}
	if got := firstInt(reNum, `height="10"`); got != 0 {
		t.Errorf("firstInt no-match = %d, want 0", got)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 42: "42", 1000: "1000"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
