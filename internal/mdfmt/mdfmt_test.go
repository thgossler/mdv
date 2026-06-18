package mdfmt

import (
	"regexp"
	"strings"
	"testing"
)

// stripANSI removes OSC 8 hyperlink sequences and SGR color codes, leaving only
// the visible text. It is used to assert that hidden URLs do not leak into the
// rendered text.
func stripANSI(s string) string {
	s = regexp.MustCompile("\x1b\\]8;;[^\x07]*\x07").ReplaceAllString(s, "")
	s = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, "")
	return s
}

func TestConvertHTMLHeadingsAndParagraph(t *testing.T) {
	in := `<h1 align="center">Title</h1>
<p>First paragraph.</p>
<h2>Sub</h2>
<p>Second<br />line.</p>`
	out := convertHTML(in)
	if !strings.Contains(out, "# Title") {
		t.Errorf("h1 not converted to '# ': %q", out)
	}
	if !strings.Contains(out, "## Sub") {
		t.Errorf("h2 not converted to '## ': %q", out)
	}
	// Paragraph should be separated by blank lines.
	if !strings.Contains(out, "\n\nFirst paragraph.\n\n") {
		t.Errorf("p not surrounded by blank lines: %q", out)
	}
	if !strings.Contains(out, "Second\nline.") {
		t.Errorf("<br> not converted to newline: %q", out)
	}
}

func TestConvertHTMLAnchorAndImage(t *testing.T) {
	in := `<a href="https://example.com"><img alt="Logo" src="x.png"></a> and <strong>bold</strong>`
	out := convertHTML(in)
	if !strings.Contains(out, "[Logo](https://example.com)") {
		t.Errorf("anchor+img not converted: %q", out)
	}
	if !strings.Contains(out, "**bold**") {
		t.Errorf("strong not converted: %q", out)
	}
}

func TestConvertHTMLLeavesCodeProtectedByCaller(t *testing.T) {
	// convertHTML itself does not protect code; Render does. Verify Render keeps
	// HTML inside fenced code blocks intact.
	in := "Text <h1>Head</h1>\n\n```\n<h1>literal</h1>\n```\n"
	out, err := Render(in, 80, "notty", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<h1>literal</h1>") {
		t.Errorf("HTML inside code fence was altered: %q", out)
	}
}

func TestHyperlinksHideURL(t *testing.T) {
	in := "See [Example](https://example.com/very/long/path) now.\n"
	out, err := Render(in, 80, "notty", true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b]8;;https://example.com/very/long/path\x07") {
		t.Errorf("missing OSC 8 opener: %q", out)
	}
	if !strings.Contains(out, "\x1b]8;;\x07") {
		t.Errorf("missing OSC 8 closer: %q", out)
	}
	if strings.Contains(out, linkStart) || strings.Contains(out, linkEnd) {
		t.Errorf("sentinels leaked into output: %q", out)
	}
	if !strings.Contains(out, "Example") {
		t.Errorf("link text missing: %q", out)
	}
}

func TestHyperlinksDisabledKeepsURL(t *testing.T) {
	in := "See [Example](https://example.com) now.\n"
	out, err := Render(in, 80, "notty", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("OSC 8 emitted when hyperlinks disabled: %q", out)
	}
	if !strings.Contains(out, "https://example.com") {
		t.Errorf("URL should remain visible when hyperlinks disabled: %q", out)
	}
}

func TestHyperlinksImageInLinkUsesAlt(t *testing.T) {
	in := "[![Platforms](https://img.shields.io/badge.svg)](https://github.com/owner/repo)\n"
	out, err := Render(in, 80, "notty", true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b]8;;https://github.com/owner/repo\x07") {
		t.Errorf("image-in-link should hyperlink to outer URL: %q", out)
	}
	if strings.Contains(out, "img.shields.io") {
		t.Errorf("badge image URL should not be visible: %q", out)
	}
	if !strings.Contains(out, "Platforms") {
		t.Errorf("badge alt text should be shown: %q", out)
	}
}

func TestHyperlinksOrderPreserved(t *testing.T) {
	in := "[one](https://a.example) and [two](https://b.example)\n"
	out, err := Render(in, 80, "notty", true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	ia := strings.Index(out, "https://a.example")
	ib := strings.Index(out, "https://b.example")
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("link URLs out of order: a=%d b=%d out=%q", ia, ib, out)
	}
}

func TestHyperlinksBalancedParensInURL(t *testing.T) {
	// URLs containing balanced parentheses (Wikipedia, Microsoft Learn and chat
	// deep links) must be consumed whole. A naive parser that stops at the first
	// ')' would leave the URL tail in the visible text, which counts toward the
	// wrap width and produces spurious early line breaks.
	in := "See [MSAL](https://learn.microsoft.com/dotnet/api/microsoft.identity.client_(msal)) now.\n"
	out, err := Render(in, 80, "notty", true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b]8;;https://learn.microsoft.com/dotnet/api/microsoft.identity.client_(msal)\x07"
	if !strings.Contains(out, want) {
		t.Errorf("balanced-paren URL not captured whole: %q", out)
	}
	// Strip OSC 8 sequences and SGR codes; the visible text must not leak any
	// part of the URL.
	visible := stripANSI(out)
	if strings.Contains(visible, "learn.microsoft.com") || strings.Contains(visible, "(msal)") {
		t.Errorf("URL leaked into visible text: %q", visible)
	}
}

func TestHyperlinksRelativeLinkBecomesFileURI(t *testing.T) {
	in := "See [Other](sub/other.md) here.\n"
	out, err := Render(in, 80, "notty", true, nil, "/docs/root")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b]8;;file:///docs/root/sub/other.md\x07") {
		t.Errorf("relative link not anchored at baseDir as file URI: %q", out)
	}
}

func TestHyperlinksRelativeLinkPreservesFragment(t *testing.T) {
	in := "See [Section](other.md#intro) here.\n"
	out, err := Render(in, 80, "notty", true, nil, "/docs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b]8;;file:///docs/other.md#intro\x07") {
		t.Errorf("fragment not preserved on file URI: %q", out)
	}
}

func TestHyperlinksAbsoluteURLUnchangedWithBaseDir(t *testing.T) {
	in := "See [Site](https://example.com/path) here.\n"
	out, err := Render(in, 80, "notty", true, nil, "/docs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b]8;;https://example.com/path\x07") {
		t.Errorf("absolute URL should be left unchanged: %q", out)
	}
}

func TestStandaloneImageNotHyperlinked(t *testing.T) {
	in := "![alt text](https://img.example/pic.png)\n"
	src, urls := prepareHyperlinks(in)
	if len(urls) != 0 {
		t.Errorf("standalone image should not be collected as a link: %v", urls)
	}
	if !strings.Contains(src, "![alt text](https://img.example/pic.png)") {
		t.Errorf("standalone image was rewritten: %q", src)
	}
}

func TestProtectRestoreCodeRoundTrip(t *testing.T) {
	in := "a `code` b\n\n```\nblock\n```\n"
	s, stash := protectCode(in)
	if strings.Contains(s, "code") || strings.Contains(s, "block") {
		t.Errorf("code not protected: %q", s)
	}
	if got := restoreCode(s, stash); got != in {
		t.Errorf("round trip mismatch:\n got %q\nwant %q", got, in)
	}
}

func TestCompactTablesNarrowsColumns(t *testing.T) {
	md := "| Flag | Description |\n| --- | --- |\n| x | Force the interactive terminal UI |\n| y | Force the graphical UI |\n"
	out, err := Render(md, 120, "notty", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	maxW := 0
	for _, l := range strings.Split(out, "\n") {
		if w := len([]rune(strings.TrimRight(l, " "))); w > maxW {
			maxW = w
		}
	}
	// Natural width is well under the 120-column wrap width; without
	// compaction the table would stretch close to 120.
	if maxW > 60 {
		t.Errorf("table not compacted: widest line = %d cols\n%s", maxW, out)
	}
	// Column structure is preserved.
	if !strings.Contains(out, "Force the interactive terminal UI") {
		t.Errorf("cell content lost: %q", out)
	}
	if !strings.Contains(out, "|") {
		t.Errorf("column separator lost: %q", out)
	}
}

func TestCompactTablesLeavesProseUntouched(t *testing.T) {
	md := "Just a paragraph with a | pipe in it, no table here.\n"
	out, err := Render(md, 120, "notty", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| pipe in it") {
		t.Errorf("prose pipe was altered: %q", out)
	}
}
