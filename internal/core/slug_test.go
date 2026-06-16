package core

import "testing"

func TestBaseSlug(t *testing.T) {
	cases := map[string]string{
		"Hello World":       "hello-world",
		"Hello, World!":     "hello-world",
		"Features":          "features",
		"C++ and C#":        "c-and-c",
		"Привет Мир":        "привет-мир",
		"Heading   spaces":  "heading---spaces",
		"under_score-dash":  "under_score-dash",
		"  Trim Me  ":       "trim-me",
		"Emoji 😀 Here":      "emoji--here",
		"1. Numbered Title": "1-numbered-title",
	}
	for in, want := range cases {
		if got := BaseSlug(in); got != want {
			t.Errorf("BaseSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSluggerDuplicates(t *testing.T) {
	s := NewSlugger()
	seq := []string{"Intro", "Intro", "Intro"}
	want := []string{"intro", "intro-1", "intro-2"}
	for i, h := range seq {
		if got := s.Slug(h); got != want[i] {
			t.Errorf("Slug #%d(%q) = %q, want %q", i, h, got, want[i])
		}
	}
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.2.0", "v1.10.0", true},
		{"1.0.0", "1.0.0", false},
		{"v2.0.0", "v1.9.9", false},
		{"v1.0.0-beta", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-beta", false},
		{"garbage", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := VersionLess(c.a, c.b); got != c.want {
			t.Errorf("VersionLess(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
