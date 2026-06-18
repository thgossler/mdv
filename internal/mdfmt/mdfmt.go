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
	neturl "net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
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

	reSentinel = regexp.MustCompile("(?s)\x01(.*?)\x02")
)

// Render converts markdown to ANSI text. HTML normalization always runs. When
// hyperlinks is true, links are emitted as OSC 8 escape sequences so only their
// text is shown; otherwise glamour renders links in its usual text+URL form.
//
// When images is non-nil, standalone images are rendered into terminal cell
// blocks (graphics or half-blocks) in place; images that cannot be rendered
// keep their alt text.
//
// style is "auto"/"" for automatic light/dark detection, or a glamour standard
// style name such as "dark", "light" or "notty". width is the wrap column.
//
// baseDir is the directory of the source document. When set, local link targets
// (relative or absolute filesystem paths, not URLs with a scheme) are rewritten
// into absolute file:// URIs anchored at baseDir, so OSC 8 hyperlinks resolve
// against the document's location rather than the terminal's working directory.
func Render(markdown string, width int, style string, hyperlinks bool, images ImageRenderer, baseDir string) (string, error) {
	src, code := protectCode(markdown)

	var imgSubs map[string]string
	if images != nil {
		src, imgSubs = extractImages(src, images, width)
	}

	src = convertHTML(src)

	var urls []string
	if hyperlinks {
		src, urls = prepareHyperlinks(src)
		if baseDir != "" {
			for i := range urls {
				urls[i] = absoluteFileURL(urls[i], baseDir)
			}
		}
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
	out = compactTables(out)
	if len(imgSubs) > 0 {
		out = substituteImages(out, imgSubs)
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
//
// Links are parsed with a hand-written scanner rather than a regex so that
// destinations containing balanced parentheses (common in Wikipedia, Microsoft
// Learn and chat deep links) are consumed whole. A regex that stops at the
// first ')' would leave the tail of such a URL in the visible text, which then
// counts toward glamour's wrap width and produces spurious early line breaks.
func prepareHyperlinks(s string) (string, []string) {
	var urls []string
	var b strings.Builder

	add := func(text, url string) {
		urls = append(urls, url)
		b.WriteString(linkStart)
		b.WriteString(text)
		b.WriteString(linkEnd)
	}

	i, n := 0, len(s)
	for i < n {
		switch s[i] {
		case '<':
			if u, end, ok := parseAutolink(s, i); ok {
				add(u, u)
				i = end
				continue
			}
		case '!':
			// Standalone image ![alt](src): leave verbatim for glamour (or the
			// image renderer, which has already substituted placeholders).
			if i+1 < n && s[i+1] == '[' {
				if _, _, end, ok := parseBracketLink(s, i+1); ok {
					b.WriteString(s[i:end])
					i = end
					continue
				}
			}
		case '[':
			if text, dest, end, ok := parseBracketLink(s, i); ok {
				url := cleanURL(dest)
				if url == "" || strings.HasPrefix(url, "#") {
					b.WriteString(s[i:end]) // anchor or empty: leave for glamour
				} else {
					if strings.TrimSpace(text) == "" {
						text = url
					}
					add(collapseLinkText(text), url)
				}
				i = end
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), urls
}

// collapseLinkText normalises a hyperlink's visible text. CommonMark turns soft
// line breaks and internal whitespace runs inside link text into single spaces,
// but mdv's lightweight parser keeps the raw source. Left as-is, a link whose
// [text] spans multiple source lines keeps its embedded newline and indentation
// and renders as a spurious mid-link line break, so collapse the whitespace.
func collapseLinkText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// parseBracketLink parses an inline link "[text](dest)" starting at s[i] == '['.
// The link text may itself be a single image "![alt](src)", in which case the
// image's alt text becomes the visible text. It returns the visible text, the
// raw destination (which may include a title), the index just past the closing
// ')', and whether a well-formed link was found.
func parseBracketLink(s string, i int) (text, dest string, end int, ok bool) {
	if i >= len(s) || s[i] != '[' {
		return "", "", 0, false
	}
	// Find the ']' that closes the link text, tracking bracket depth so nested
	// image brackets in the text do not close it early.
	depth := 0
	j := i
	for j < len(s) {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				goto closed
			}
		}
		j++
	}
	return "", "", 0, false
closed:
	rawText := s[i+1 : j]
	if j+1 >= len(s) || s[j+1] != '(' {
		return "", "", 0, false // not an inline link (e.g. a reference link)
	}
	d, dend, dok := parseDest(s, j+1)
	if !dok {
		return "", "", 0, false
	}
	vis := rawText
	if strings.HasPrefix(rawText, "!") {
		if alt, _, aend, aok := parseBracketLink(rawText, 1); aok && aend == len(rawText) {
			vis = alt // image-in-link: show the image's alt text
		}
	}
	return vis, d, dend, true
}

// parseDest parses a link destination "(dest)" starting at s[i] == '('. Nested
// parentheses are balanced so URLs that legitimately contain them are kept
// intact. The returned destination still includes any optional title; cleanURL
// strips that.
func parseDest(s string, i int) (dest string, end int, ok bool) {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[i+1 : j], j + 1, true
			}
		}
	}
	return "", 0, false
}

// parseAutolink parses an autolink "<scheme:...>" starting at s[i] == '<'. Only
// http(s) and mailto autolinks are treated as hyperlinks, matching the prior
// behavior; anything else is left for glamour.
func parseAutolink(s string, i int) (url string, end int, ok bool) {
	j := strings.IndexByte(s[i:], '>')
	if j < 0 {
		return "", 0, false
	}
	inner := s[i+1 : i+j]
	if inner == "" || strings.ContainsAny(inner, " \t\n") {
		return "", 0, false
	}
	if !strings.HasPrefix(inner, "http://") &&
		!strings.HasPrefix(inner, "https://") &&
		!strings.HasPrefix(inner, "mailto:") {
		return "", 0, false
	}
	return inner, i + j + 1, true
}

func cleanURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "<") {
		if j := strings.IndexByte(u, '>'); j >= 0 {
			return u[1:j] // angle-bracket destination: keep its contents verbatim
		}
		return strings.TrimPrefix(u, "<")
	}
	if i := strings.IndexAny(u, " \t"); i >= 0 {
		u = u[:i] // drop an optional inline title: (url "title")
	}
	return u
}

// absoluteFileURL converts a local link target (a relative or absolute
// filesystem path) into an absolute file:// URI anchored at baseDir. Targets
// that carry a URL scheme (http:, mailto:, ...), protocol-relative URLs (//host)
// and pure fragments (#anchor) are returned unchanged. This makes terminal
// OSC 8 hyperlinks resolve against the document's directory regardless of the
// platform path separator or the process working directory.
func absoluteFileURL(raw, baseDir string) string {
	u := strings.TrimSpace(raw)
	if u == "" || strings.HasPrefix(u, "#") || strings.HasPrefix(u, "//") || hasURLScheme(u) {
		return raw
	}
	path, frag := u, ""
	if k := strings.IndexByte(u, '#'); k >= 0 {
		path, frag = u[:k], u[k:]
	}
	if path == "" {
		return raw
	}
	if dec, err := neturl.PathUnescape(path); err == nil {
		path = dec
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	path = filepath.Clean(path)
	return fileURI(path) + frag
}

// hasURLScheme reports whether u begins with a URL scheme such as "https:".
// A single leading letter followed by ':' (a Windows drive, e.g. "C:\\...") is
// not treated as a scheme.
func hasURLScheme(u string) bool {
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c == ':':
			return i > 1
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			// scheme character
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
			// scheme character (not allowed as the first character)
		default:
			return false
		}
	}
	return false
}

// fileURI builds an absolute file:// URI from a cleaned absolute filesystem
// path, percent-encoding as needed. On Windows the drive-letter path "C:\\x"
// becomes "file:///C:/x".
func fileURI(p string) string {
	s := filepath.ToSlash(p)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	u := neturl.URL{Scheme: "file", Path: s}
	return u.String()
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

// compactTables narrows tables that glamour has stretched to the full wrap
// width. glamour (v0.10) sizes every table to the render width, so a table with
// short cells gets padded with large runs of spaces, which is hard to read in
// wide terminals. This walks the rendered output, finds table blocks (a header
// rule plus the data rows around it) and re-pads each column to the natural
// width of its widest cell, collapsing the excess inter-column padding while
// keeping the columns aligned.
//
// Only left-aligned padding (the glamour default) is compacted; right- and
// centre-aligned columns are left untouched so nothing is mis-rendered. Any
// block that does not look like a well-formed table is returned verbatim.
func compactTables(s string) string {
	if !strings.Contains(s, "│") && !strings.Contains(s, "|") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		if !isTableLine(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i
		for j < len(lines) && isTableLine(lines[j]) {
			j++
		}
		out = append(out, compactBlock(lines[i:j])...)
		i = j
	}
	return strings.Join(out, "\n")
}

// isTableLine reports whether a rendered line might be part of a table: it
// contains a column separator or is a horizontal header rule.
func isTableLine(line string) bool {
	p := ansi.Strip(line)
	return strings.ContainsRune(p, '│') || strings.ContainsRune(p, '|') || isRuleRow(p)
}

// isRuleRow reports whether an ANSI-stripped line is a table header rule, i.e.
// it consists solely of horizontal/joint glyphs (and spaces) with enough dashes
// to be a rule rather than incidental punctuation.
func isRuleRow(p string) bool {
	t := strings.TrimSpace(p)
	if t == "" {
		return false
	}
	dashes := 0
	for _, r := range t {
		switch r {
		case '─', '-':
			dashes++
		case '┼', '┬', '┴', '├', '┤', '│', '|', '+', ' ':
		default:
			return false
		}
	}
	return dashes >= 3
}

// compactBlock re-pads a run of candidate table lines. If the run is not a
// well-formed table (a header rule plus data rows that all share the same
// column count) it is returned unchanged.
func compactBlock(block []string) []string {
	styled := false
	for _, l := range block {
		p := ansi.Strip(l)
		if strings.ContainsRune(p, '│') || strings.ContainsRune(p, '─') || strings.ContainsRune(p, '┼') {
			styled = true
			break
		}
	}
	bar, horiz, joint := "|", "-", "|"
	if styled {
		bar, horiz, joint = "│", "─", "┼"
	}

	// Common left indentation (table margin) across the block.
	indent := -1
	for _, l := range block {
		p := ansi.Strip(l)
		n := len(p) - len(strings.TrimLeft(p, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent < 0 {
		indent = 0
	}

	var dataIdx, ruleIdx []int
	barCount := -1
	for k, l := range block {
		p := ansi.Strip(l)
		switch {
		case isRuleRow(p):
			ruleIdx = append(ruleIdx, k)
		case strings.Contains(p, bar):
			c := strings.Count(p, bar)
			if barCount < 0 {
				barCount = c
			} else if c != barCount {
				return block
			}
			dataIdx = append(dataIdx, k)
		default:
			return block
		}
	}
	if len(ruleIdx) == 0 || len(dataIdx) == 0 || barCount < 1 {
		return block
	}
	ncol := barCount + 1

	// Split each data row into column segments and measure the widest cell.
	segRows := make([][]string, len(dataIdx))
	maxC := make([]int, ncol)
	for ri, k := range dataIdx {
		segs := strings.Split(dropLeadingSpaces(block[k], indent), bar)
		if len(segs) != ncol {
			return block
		}
		segRows[ri] = segs
		for c := 0; c < ncol; c++ {
			if w := cellWidth(segs[c]); w > maxC[c] {
				maxC[c] = w
			}
		}
	}

	res := make([]string, len(block))
	copy(res, block)
	indentStr := strings.Repeat(" ", indent)

	for ri, k := range dataIdx {
		var b strings.Builder
		b.WriteString(indentStr)
		for c, seg := range segRows[ri] {
			t := cellWidth(seg)
			b.WriteString(ansi.Truncate(seg, t, ""))
			if styled {
				b.WriteString("\x1b[0m")
			}
			if c < ncol-1 {
				b.WriteString(strings.Repeat(" ", maxC[c]-t))
				b.WriteString(" ")
				b.WriteString(bar)
			}
		}
		res[k] = strings.TrimRight(b.String(), " ")
	}

	for _, k := range ruleIdx {
		var b strings.Builder
		b.WriteString(indentStr)
		for c := 0; c < ncol; c++ {
			b.WriteString(strings.Repeat(horiz, maxC[c]+1))
			if c < ncol-1 {
				b.WriteString(joint)
			}
		}
		res[k] = b.String()
	}
	return res
}

// cellWidth returns the display width of a column segment with trailing padding
// removed, i.e. the width its content actually needs.
func cellWidth(seg string) int {
	return ansi.StringWidth(strings.TrimRight(ansi.Strip(seg), " "))
}

// dropLeadingSpaces removes up to n leading ASCII spaces from s. Rendered table
// lines begin with plain (unstyled) margin spaces, so this trims the shared
// indent without disturbing any escape sequences.
func dropLeadingSpaces(s string, n int) string {
	i := 0
	for i < len(s) && i < n && s[i] == ' ' {
		i++
	}
	return s[i:]
}
