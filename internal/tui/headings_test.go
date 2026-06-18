package tui

import "testing"

func TestCollectHeadings(t *testing.T) {
	md := "" +
		"# Title\n" +
		"intro\n" +
		"## Section One ##\n" +
		"```\n" +
		"# Not A Heading In Code\n" +
		"```\n" +
		"### Sub-section\n" +
		"~~~\n" +
		"## Also Not A Heading\n" +
		"~~~\n" +
		"#NotAHeading (no space)\n"

	hs := collectHeadings(md)

	if len(hs) != 3 {
		t.Fatalf("collectHeadings returned %d headings, want 3: %+v", len(hs), hs)
	}
	want := []Heading{
		{Level: 1, Text: "Title"},
		{Level: 2, Text: "Section One"}, // trailing ## stripped by reATX
		{Level: 3, Text: "Sub-section"},
	}
	for i, w := range want {
		if hs[i].Level != w.Level || hs[i].Text != w.Text {
			t.Errorf("heading %d = {%d,%q}, want {%d,%q}", i, hs[i].Level, hs[i].Text, w.Level, w.Text)
		}
	}
}

func TestCollectHeadingsEmpty(t *testing.T) {
	if hs := collectHeadings("just paragraph text\nmore text\n"); len(hs) != 0 {
		t.Errorf("collectHeadings on heading-less doc = %+v, want empty", hs)
	}
}

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m":               "red",
		"plain":                            "plain",
		"\x1b[1;33mbold yellow\x1b[0m end": "bold yellow end",
		"":                                 "",
	}
	for in, want := range cases {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}
