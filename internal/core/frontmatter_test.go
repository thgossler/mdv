package core

import (
	"reflect"
	"testing"
)

func TestExtractFrontmatterBasic(t *testing.T) {
	md := "---\ntitle: Hello World\nauthor: Jane Doe\ndate: 2024-01-02\ntags: [go, markdown]\nstatus: draft\n---\n# Body\n\ntext\n"
	fm, body := ExtractFrontmatter(md)
	if !fm.Has {
		t.Fatal("expected Has=true")
	}
	if fm.Title != "Hello World" {
		t.Errorf("Title = %q", fm.Title)
	}
	if fm.Author != "Jane Doe" {
		t.Errorf("Author = %q", fm.Author)
	}
	if fm.Date != "2024-01-02" {
		t.Errorf("Date = %q", fm.Date)
	}
	if !reflect.DeepEqual(fm.Tags, []string{"go", "markdown"}) {
		t.Errorf("Tags = %#v", fm.Tags)
	}
	if len(fm.Fields) != 1 || fm.Fields[0].Key != "status" || fm.Fields[0].Value != "draft" {
		t.Errorf("Fields = %#v", fm.Fields)
	}
	if body != "# Body\n\ntext\n" {
		t.Errorf("body = %q", body)
	}
}

func TestExtractFrontmatterNone(t *testing.T) {
	md := "# Just a heading\n\nNo front matter.\n"
	fm, body := ExtractFrontmatter(md)
	if fm.Has {
		t.Error("expected Has=false")
	}
	if body != md {
		t.Errorf("body changed: %q", body)
	}
}

func TestExtractFrontmatterThematicBreak(t *testing.T) {
	// A leading horizontal rule around a paragraph is not a YAML mapping and
	// must be left untouched.
	md := "---\n\nJust a paragraph between rules.\n\n---\n"
	fm, body := ExtractFrontmatter(md)
	if fm.Has {
		t.Errorf("thematic break misparsed as front matter: %#v", fm)
	}
	if body != md {
		t.Errorf("body changed: %q", body)
	}
}

func TestExtractFrontmatterInvalidYAML(t *testing.T) {
	md := "---\ntitle: [unclosed\n---\nbody\n"
	fm, body := ExtractFrontmatter(md)
	if fm.Has {
		t.Error("invalid YAML should not be treated as front matter")
	}
	if body != md {
		t.Errorf("body changed: %q", body)
	}
}

func TestExtractFrontmatterOrderAndNested(t *testing.T) {
	md := "---\nfirst: 1\nsecond:\n  a: x\n  b: y\nthird: [p, q]\n---\nbody\n"
	fm, _ := ExtractFrontmatter(md)
	if len(fm.Fields) != 3 {
		t.Fatalf("Fields = %#v", fm.Fields)
	}
	if fm.Fields[0].Key != "first" || fm.Fields[0].Value != "1" {
		t.Errorf("field 0 = %#v", fm.Fields[0])
	}
	if fm.Fields[1].Key != "second" || fm.Fields[1].Value != "a: x, b: y" {
		t.Errorf("field 1 = %#v", fm.Fields[1])
	}
	if fm.Fields[2].Key != "third" || fm.Fields[2].Value != "p, q" {
		t.Errorf("field 2 = %#v", fm.Fields[2])
	}
}

func TestExtractFrontmatterAuthorsList(t *testing.T) {
	md := "---\nauthors:\n  - Ann\n  - Bob\n---\nbody\n"
	fm, _ := ExtractFrontmatter(md)
	if fm.Author != "Ann, Bob" {
		t.Errorf("Author = %q", fm.Author)
	}
}

func TestExtractFrontmatterCRLFAndBOM(t *testing.T) {
	md := "\uFEFF---\r\ntitle: Win\r\n---\r\nbody\r\n"
	fm, body := ExtractFrontmatter(md)
	if !fm.Has || fm.Title != "Win" {
		t.Errorf("fm = %#v", fm)
	}
	if body != "body\r\n" {
		t.Errorf("body = %q", body)
	}
}

func TestStripFrontmatter(t *testing.T) {
	md := "---\ntitle: X\n---\nbody\n"
	if got := StripFrontmatter(md); got != "body\n" {
		t.Errorf("StripFrontmatter = %q", got)
	}
}

func TestExtractFrontmatterAfterLeadingComment(t *testing.T) {
	md := "<!-- DocID: 5983 (don't change or remove this comment!) -->\n\n\n---\ntitle: Home\nowners:\n  - a@example.com\n---\n# Welcome\n"
	fm, body := ExtractFrontmatter(md)
	if !fm.Has {
		t.Fatal("expected Has=true when front matter follows a leading HTML comment")
	}
	if fm.Title != "Home" {
		t.Errorf("Title = %q", fm.Title)
	}
	// The inert marker comment must be preserved in the body, while the YAML
	// block is removed.
	if want := "<!-- DocID: 5983 (don't change or remove this comment!) -->\n\n\n# Welcome\n"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestExtractFrontmatterAfterLeadingWhitespace(t *testing.T) {
	md := "\n\n---\ntitle: T\n---\nbody\n"
	fm, _ := ExtractFrontmatter(md)
	if !fm.Has || fm.Title != "T" {
		t.Errorf("fm = %#v", fm)
	}
}

func TestExtractFrontmatterCommentOnlyNoFrontmatter(t *testing.T) {
	// A leading comment with no following front matter must leave the document
	// untouched.
	md := "<!-- note -->\n\n# Heading\n\ntext\n"
	fm, body := ExtractFrontmatter(md)
	if fm.Has {
		t.Errorf("expected Has=false, got %#v", fm)
	}
	if body != md {
		t.Errorf("body changed: %q", body)
	}
}
