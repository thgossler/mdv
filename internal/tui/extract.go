package tui

import (
	"regexp"
	"strings"
)

// Link is a navigable reference discovered in a markdown document.
type Link struct {
	Text string // visible label
	Href string // raw destination as written
}

var (
	reInline     = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	reWiki       = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	reAutolink   = regexp.MustCompile(`<((?:https?|mailto):[^>\s]+)>`)
	reBareURL    = regexp.MustCompile(`(?:^|\s)(https?://[^\s)<>\]]+)`)
	reImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reFence      = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	reInlineCode = regexp.MustCompile("`[^`]*`")
)

// ExtractLinks returns the navigable links in a markdown document in reading
// order, de-duplicated by destination while keeping the first label. Code spans
// and fenced code blocks are ignored so example links are not offered.
func ExtractLinks(markdown string) []Link {
	clean := reFence.ReplaceAllString(markdown, "")
	clean = reInlineCode.ReplaceAllString(clean, "")
	clean = reImage.ReplaceAllString(clean, "")

	var links []Link
	seen := make(map[string]bool)

	add := func(text, href string) {
		href = strings.TrimSpace(href)
		// Strip an optional inline title: (url "title").
		if i := strings.IndexAny(href, " \t"); i >= 0 {
			href = href[:i]
		}
		href = strings.Trim(href, "<>")
		if href == "" || seen[href+"\x00"+text] {
			return
		}
		seen[href+"\x00"+text] = true
		if strings.TrimSpace(text) == "" {
			text = href
		}
		links = append(links, Link{Text: collapseSpace(text), Href: href})
	}

	for _, m := range reInline.FindAllStringSubmatch(clean, -1) {
		add(m[1], m[2])
	}
	for _, m := range reWiki.FindAllStringSubmatch(clean, -1) {
		inner := m[1]
		label := inner
		if i := strings.Index(inner, "|"); i >= 0 {
			label = inner[i+1:]
		}
		add(label, "[["+inner+"]]")
	}
	for _, m := range reAutolink.FindAllStringSubmatch(clean, -1) {
		add(m[1], m[1])
	}
	for _, m := range reBareURL.FindAllStringSubmatch(clean, -1) {
		add(m[1], m[1])
	}
	return links
}

// Heading is a document heading with its level and GitHub-style slug.
type Heading struct {
	Level int
	Text  string
	Slug  string
}

var reATX = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
