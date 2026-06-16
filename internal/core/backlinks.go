package core

import (
	"bufio"
	"os"
	"strings"
)

// Backlink is a reference from one workspace document to another.
type Backlink struct {
	// SourcePath is the absolute path of the document containing the link.
	SourcePath string `json:"sourcePath"`
	// SourceName is the base file name of the source document.
	SourceName string `json:"sourceName"`
	// SourceTitle is the H1 title of the source document, if known.
	SourceTitle string `json:"sourceTitle"`
	// Line is the 1-based line number where the link occurs.
	Line int `json:"line"`
	// Snippet is the trimmed source line for context.
	Snippet string `json:"snippet"`
}

// FindBacklinks scans every workspace document for links that resolve to
// targetPath and returns the referencing locations with line snippets.
func FindBacklinks(targetPath string, cfg Defaults, workspace []DocFile) []Backlink {
	var out []Backlink
	target := normPath(targetPath)
	targetStem := strings.ToLower(baseStem(targetPath))

	for _, doc := range workspace {
		if normPath(doc.Path) == target {
			continue // skip self
		}
		links := scanLinks(doc.Path)
		for _, l := range links {
			if linkMatchesTarget(l.href, doc.Path, target, targetStem, cfg, workspace) {
				out = append(out, Backlink{
					SourcePath:  doc.Path,
					SourceName:  doc.Name,
					SourceTitle: doc.Title,
					Line:        l.line,
					Snippet:     l.snippet,
				})
			}
		}
	}
	return out
}

type scannedLink struct {
	href    string
	line    int
	snippet string
}

// scanLinks extracts inline-link and wikilink hrefs with their line numbers.
func scanLinks(path string) []scannedLink {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var links []scannedLink
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	inFence := false
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range reInlineLink.FindAllStringSubmatch(line, -1) {
			links = append(links, scannedLink{href: cleanHref(m[1]), line: lineNo, snippet: collapse(trimmed)})
		}
		for _, m := range reWikiLink.FindAllStringSubmatch(line, -1) {
			links = append(links, scannedLink{href: "[[" + m[1] + "]]", line: lineNo, snippet: collapse(trimmed)})
		}
	}
	return links
}

func linkMatchesTarget(href, sourcePath, target, targetStem string, cfg Defaults, workspace []DocFile) bool {
	tgt := ResolveLink(href, dirOf(sourcePath), cfg, workspace)
	switch tgt.Kind {
	case LinkMarkdown, LinkWikiInternal:
		return normPath(tgt.Resolved) == target
	case LinkBroken:
		// A broken wikilink may still name the target by stem.
		return strings.ToLower(strings.TrimSpace(tgt.Resolved)) == targetStem
	default:
		return false
	}
}

func cleanHref(h string) string {
	h = strings.TrimSpace(h)
	if i := strings.IndexAny(h, " \t"); i >= 0 {
		h = h[:i]
	}
	return strings.Trim(h, "<>")
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func normPath(p string) string { return strings.ToLower(cleanPath(p)) }
