// Package mdfmt renders markdown to ANSI text using glamour, adding two
// capabilities glamour itself lacks in this version: converting common inline
// HTML to its markdown equivalent so block structure renders correctly, and
// emitting OSC 8 terminal hyperlinks so links stay clickable without printing
// their full URLs.
//
// The hyperlink mechanism works around glamour's word-wrap (muesli/reflow),
// which does not understand OSC 8 escape sequences. Links are first replaced
// with zero-width control-character sentinels that survive word-wrapping in
// document order; after glamour renders, the sentinels are rewritten into OSC 8
// escapes with the original URLs reattached.
package mdfmt

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
)

// Sentinels marking the start and end of a hyperlink's visible text. They are
// C0 control characters that glamour passes through untouched and that reflow
// treats as zero width, so they survive word-wrapping without affecting layout.
const (
	linkStart = "\x01"
	linkEnd   = "\x02"
)

var (
	reFence      = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	reInlineCode = regexp.MustCompile("`[^`]*`")
	rePlaceholde = regexp.MustCompile("\x00(\\d+)\x00")

	reComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	reBr       = regexp.MustCompile(`(?i)<br\s*/?>`)
	reAnchor   = regexp.MustCompile(`(?is)<a\b[^>]*?\bhref\s*=\s*["']([^"']*)["'][^>]*>(.*?)</a>`)
	reImgAlt   = regexp.MustCompile(`(?is)<img\b[^>]*?\balt\s*=\s*["']([^"']*)["'][^>]*>`)
	reImgPlain = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reStrong   = regexp.MustCompile(`(?i)</?(?:strong|b)\s*>`)
	reEm       = regexp.MustCompile(`(?i)</?(?:em|i)\s*>`)
	reCodeTag  = regexp.MustCompile(`(?i)</?code\s*>`)
	rePBlock   = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	reDivBlock = regexp.MustCompile(`(?is)<div\b[^>]*>(.*?)</div>`)
	rePOpen    = regexp.MustCompile(`(?i)<p\b[^>]*>`)
	rePClose   = regexp.MustCompile(`(?i)</p\s*>`)
	reAnyTag   = regexp.MustCompile(`(?s)<[^>]+>`)
	reManyNL   = regexp.MustCompile(`\n{3,}`)

	reHeading = [7]*regexp.Regexp{
		nil,
		regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`),
		regexp.MustCompile(`(?is)<h2\b[^>]*>(.*?)</h2>`),
		regexp.MustCompile(`(?is)<h3\b[^>]*>(.*?)</h3>`),
		regexp.MustCompile(`(?is)<h4\b[^>]*>(.*?)</h4>`),
		regexp.MustCompile(`(?is)<h5\b[^>]*>(.*?)</h5>`),
		regexp.MustCompile(`(?is)<h6\b[^>]*>(.*?)</h6>`),
	}

	// reAnyLink matches, in priority order: a standalone image, an image
	// wrapped in a link, a plain inline link, and an autolink.
	reAnyLink = regexp.MustCompile(`(?s)!\[[^\]]*\]\([^)]*\)|\[!\[[^\]]*\]\([^)]*\)\]\([^)]+\)|\[[^\]]*\]\([^)]+\)|<(?:https?|mailto):[^>\s]+>`)
	reImgLink = regexp.MustCompile(`(?s)^\[!\[([^\]]*)\]\([^)]*\)\]\(([^)]+)\)$`)
	rePlain   = regexp.MustCompile(`(?s)^\[([^\]]*)\]\(([^)]+)\)$`)

	reSentinel = regexp.MustCompile("(?s)\x01(.*?)\x02")
)

// Render converts markdown to ANSI text. HTML normalization always runs. When
// hyperlinks is true, links are emitted as OSC 8 escape sequences so only their
// text is shown; otherwise glamour renders links in its usual text+URL form.
//
// style is "auto"/"" for automatic light/dark detection, or a glamour standard
// style name such as "dark", "light" or "notty". width is the wrap column.
func Render(markdown string, width int, style string, hyperlinks bool) (string, error) {
	src, code := protectCode(markdown)
	src = convertHTML(src)

	var urls []string
	if hyperlinks {
		src, urls = prepareHyperlinks(src)
	}
	src = restoreCode(src, code)

	r, err := newRenderer(style, width)
	if err != nil {
		return "", err
	}
	out, err := r.Render(src)
	if err != nil {
		return "", err
	}

	if hyperlinks {
		out = applyHyperlinks(out, urls)
	}
	return out, nil
}

func newRenderer(style string, width int) (*glamour.TermRenderer, error) {
	if width < 1 {
		width = 80
	}
	opts := []glamour.TermRendererOption{
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	}
	if style == "auto" || style == "" {
		opts = append(opts, glamour.WithAutoStyle())
	} else {
		opts = append(opts, glamour.WithStandardStyle(style))
	}
	return glamour.NewTermRenderer(opts...)
}

// protectCode replaces fenced code blocks and inline code spans with opaque
// placeholders so later transforms leave code untouched. The originals are
// returned for restoreCode.
func protectCode(s string) (string, []string) {
	var stash []string
	stashOne := func(m string) string {
		stash = append(stash, m)
		return fmt.Sprintf("\x00%d\x00", len(stash)-1)
	}
	s = reFence.ReplaceAllStringFunc(s, stashOne)
	s = reInlineCode.ReplaceAllStringFunc(s, stashOne)
	return s, stash
}

func restoreCode(s string, stash []string) string {
	return rePlaceholde.ReplaceAllStringFunc(s, func(m string) string {
		sub := rePlaceholde.FindStringSubmatch(m)
		var i int
		fmt.Sscanf(sub[1], "%d", &i)
		if i < 0 || i >= len(stash) {
			return m
		}
		return stash[i]
	})
}

// convertHTML rewrites common inline/block HTML into markdown equivalents so
// glamour formats them instead of dropping the tags.
func convertHTML(s string) string {
	s = reComment.ReplaceAllString(s, "")
	s = reBr.ReplaceAllString(s, "\n")
	s = reAnchor.ReplaceAllString(s, "[$2]($1)")
	s = reImgAlt.ReplaceAllString(s, "$1")
	s = reImgPlain.ReplaceAllString(s, "")
	s = reStrong.ReplaceAllString(s, "**")
	s = reEm.ReplaceAllString(s, "*")
	s = reCodeTag.ReplaceAllString(s, "`")
	for level := 1; level <= 6; level++ {
		s = reHeading[level].ReplaceAllString(s, "\n\n"+strings.Repeat("#", level)+" $1\n\n")
	}
	// Block containers carry the source's HTML indentation; de-indent their
	// content so a paragraph indented in the source is not misread as a markdown
	// code block, while preserving intentional line breaks (e.g. badge rows).
	s = rePBlock.ReplaceAllStringFunc(s, func(m string) string {
		sub := rePBlock.FindStringSubmatch(m)
		return "\n\n" + deindent(sub[1]) + "\n\n"
	})
	s = reDivBlock.ReplaceAllStringFunc(s, func(m string) string {
		sub := reDivBlock.FindStringSubmatch(m)
		return deindent(sub[1])
	})
	s = rePOpen.ReplaceAllString(s, "\n\n")
	s = rePClose.ReplaceAllString(s, "\n\n")
	s = reAnyTag.ReplaceAllString(s, "")
	s = unescapeEntities(s)
	s = reManyNL.ReplaceAllString(s, "\n\n")
	return s
}

// deindent removes leading horizontal whitespace from each line. It is applied
// to the inner content of block HTML elements, whose indentation is structural
// (insignificant in HTML) rather than meaningful markdown nesting.
func deindent(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimLeft(ln, " \t")
	}
	return strings.Join(lines, "\n")
}

func unescapeEntities(s string) string {
	return strings.NewReplacer(
		"&nbsp;", " ",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&amp;", "&",
	).Replace(s)
}

// prepareHyperlinks replaces markdown links with sentinel-wrapped visible text
// and returns the rewritten markdown plus the URLs in document order. Standalone
// images are left for glamour to render.
func prepareHyperlinks(s string) (string, []string) {
	var urls []string

	wrap := func(text, url, orig string) string {
		url = cleanURL(url)
		if url == "" || strings.HasPrefix(url, "#") {
			return orig
		}
		if strings.TrimSpace(text) == "" {
			text = url
		}
		urls = append(urls, url)
		return linkStart + text + linkEnd
	}

	out := reAnyLink.ReplaceAllStringFunc(s, func(m string) string {
		switch {
		case strings.HasPrefix(m, "!["):
			return m // standalone image: leave for glamour
		case strings.HasPrefix(m, "["):
			if sub := reImgLink.FindStringSubmatch(m); sub != nil {
				return wrap(sub[1], sub[2], m)
			}
			if sub := rePlain.FindStringSubmatch(m); sub != nil {
				return wrap(sub[1], sub[2], m)
			}
			return m
		case strings.HasPrefix(m, "<"):
			u := strings.Trim(m, "<>")
			return wrap(u, u, m)
		}
		return m
	})
	return out, urls
}

func cleanURL(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.IndexAny(u, " \t"); i >= 0 {
		u = u[:i] // drop an optional inline title: (url "title")
	}
	return strings.Trim(u, "<>")
}

// applyHyperlinks rewrites sentinel-wrapped text in rendered ANSI output into
// OSC 8 hyperlink escape sequences, consuming urls in order.
func applyHyperlinks(rendered string, urls []string) string {
	i := 0
	out := reSentinel.ReplaceAllStringFunc(rendered, func(m string) string {
		text := m[1 : len(m)-1] // strip the single-byte sentinels
		if i >= len(urls) {
			return text
		}
		u := urls[i]
		i++
		return "\x1b]8;;" + u + "\x07" + text + "\x1b]8;;\x07"
	})
	// Remove any stray sentinels that were not matched as a pair.
	return strings.NewReplacer(linkStart, "", linkEnd, "").Replace(out)
}
