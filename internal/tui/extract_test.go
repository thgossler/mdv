package tui

import "testing"

func TestExtractLinksKinds(t *testing.T) {
	md := "" +
		"[Inline](https://example.com/page) and [[WikiNote]] and [[note|Alias]].\n" +
		"Autolink <https://auto.example> and bare https://bare.example here.\n" +
		"An image ![pic](https://img.example/x.png) is not a link.\n" +
		"```\n[Fenced](https://fenced.example)\n```\n" +
		"Inline code `[Code](https://code.example)` skipped.\n"

	links := ExtractLinks(md)

	want := map[string]string{ // href -> text
		"https://example.com/page": "Inline",
		"[[WikiNote]]":             "WikiNote",
		"[[note|Alias]]":           "Alias",
		"https://auto.example":     "https://auto.example",
		"https://bare.example":     "https://bare.example",
	}
	got := map[string]string{}
	for _, l := range links {
		got[l.Href] = l.Text
	}
	for href, text := range want {
		if got[href] != text {
			t.Errorf("link %q: text = %q, want %q (all: %+v)", href, got[href], text, links)
		}
	}
	// Images, fenced code and inline code must be excluded.
	for _, bad := range []string{"https://img.example/x.png", "https://fenced.example", "https://code.example"} {
		if _, ok := got[bad]; ok {
			t.Errorf("excluded href %q was extracted: %+v", bad, links)
		}
	}
}

func TestExtractLinksDedupAndEmptyText(t *testing.T) {
	md := "[same](https://x.example) then [same](https://x.example) again, and [](https://y.example).\n"
	links := ExtractLinks(md)

	count := 0
	var empty *Link
	for i := range links {
		if links[i].Href == "https://x.example" {
			count++
		}
		if links[i].Href == "https://y.example" {
			empty = &links[i]
		}
	}
	if count != 1 {
		t.Errorf("duplicate link not de-duplicated: count = %d, want 1 (%+v)", count, links)
	}
	if empty == nil || empty.Text != "https://y.example" {
		t.Errorf("empty link text not replaced with href: %+v", empty)
	}
}

func TestExtractLinksTitleAndAngleStripped(t *testing.T) {
	md := "[Title](https://example.com \"a tooltip\") and [Angle](<https://angle.example>).\n"
	links := ExtractLinks(md)
	got := map[string]bool{}
	for _, l := range links {
		got[l.Href] = true
	}
	if !got["https://example.com"] {
		t.Errorf("inline title not stripped from href: %+v", links)
	}
	if !got["https://angle.example"] {
		t.Errorf("angle brackets not stripped from href: %+v", links)
	}
}

func TestCollapseSpace(t *testing.T) {
	cases := map[string]string{
		"a  b":         "a b",
		"  trim  me  ": "trim me",
		"line\nbreak":  "line break",
		"tab\tsep":     "tab sep",
		"single":       "single",
		"":             "",
	}
	for in, want := range cases {
		if got := collapseSpace(in); got != want {
			t.Errorf("collapseSpace(%q) = %q, want %q", in, got, want)
		}
	}
}
