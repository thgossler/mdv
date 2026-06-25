package core

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// fmRe matches a leading YAML front matter block: a `---` fence (optionally
// preceded by a UTF-8 BOM and any number of HTML comments and surrounding
// whitespace), arbitrary YAML, and a closing `---` fence on its own line. The
// leading-comment allowance lets documents carry an inert marker comment (for
// example a generated DocID) above their front matter. Capture group 1 holds
// that preserved prefix; group 2 holds the YAML. It mirrors the regex used by
// the GUI frontend so every run mode recognizes front matter identically.
var fmRe = regexp.MustCompile(`^\x{FEFF}?((?:\s*<!--[\s\S]*?-->)*\s*)---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)`)

// FrontmatterField is a single key/value entry preserved in document order. The
// value is pre-formatted for display.
type FrontmatterField struct {
	Key   string
	Value string
}

// Frontmatter holds the parsed metadata from a document's leading YAML block.
//
// Title, Author and Date carry the recognized "headline" fields (when present)
// so each surface can present them prominently. Fields lists every remaining
// top-level entry in document order so nothing is hidden. Has reports whether a
// valid front matter mapping was found at all.
type Frontmatter struct {
	Title  string
	Author string
	Date   string
	Tags   []string
	Fields []FrontmatterField
	Has    bool
}

// recognizedKeys are the keys promoted into the headline fields; they are
// omitted from Fields to avoid showing the same information twice.
var recognizedKeys = map[string]bool{
	"title":    true,
	"author":   true,
	"authors":  true,
	"date":     true,
	"updated":  true,
	"tags":     true,
	"keywords": true,
}

// ExtractFrontmatter splits a leading YAML `--- ... ---` block from the markdown
// body. The block is only treated as front matter when it parses to a YAML
// mapping, mirroring the frontend so a lone `---` thematic break or a non-object
// document is left untouched. The returned body is the markdown with the block
// (and its trailing newline) removed; when no front matter is present the
// original markdown is returned unchanged with Has set to false.
func ExtractFrontmatter(markdown string) (Frontmatter, string) {
	m := fmRe.FindStringSubmatch(markdown)
	if m == nil {
		return Frontmatter{}, markdown
	}

	var node yaml.Node
	if err := yaml.Unmarshal([]byte(m[2]), &node); err != nil {
		return Frontmatter{}, markdown
	}
	doc := mappingNode(&node)
	if doc == nil {
		return Frontmatter{}, markdown
	}

	// Preserve any leading comment/whitespace prefix in the returned body so an
	// inert marker comment above the front matter is not silently dropped.
	body := m[1] + markdown[len(m[0]):]
	fm := Frontmatter{Has: true}

	// A mapping node stores keys and values as alternating children.
	for i := 0; i+1 < len(doc.Content); i += 2 {
		keyNode := doc.Content[i]
		valNode := doc.Content[i+1]
		key := keyNode.Value
		lower := strings.ToLower(strings.TrimSpace(key))

		switch lower {
		case "title":
			fm.Title = scalarString(valNode)
		case "author", "authors":
			if fm.Author == "" {
				fm.Author = joinNode(valNode)
			}
		case "date", "updated":
			if fm.Date == "" {
				fm.Date = scalarString(valNode)
			}
		case "tags", "keywords":
			if len(fm.Tags) == 0 {
				fm.Tags = sequenceStrings(valNode)
			}
		default:
			fm.Fields = append(fm.Fields, FrontmatterField{
				Key:   key,
				Value: formatNode(valNode),
			})
		}
	}

	return fm, body
}

// StripFrontmatter returns the markdown body with any leading YAML front matter
// removed. It is a thin wrapper around ExtractFrontmatter for callers that only
// need the body.
func StripFrontmatter(markdown string) string {
	_, body := ExtractFrontmatter(markdown)
	return body
}

// mappingNode unwraps a document node to its underlying mapping, or returns nil
// when the top-level YAML value is not a mapping.
func mappingNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return nil
	}
	return n
}

// scalarString returns a trimmed string form of a scalar node, or "" for
// non-scalar nodes.
func scalarString(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(n.Value)
}

// joinNode renders a scalar as-is or a sequence as a comma-separated list, used
// for author fields that may be a single name or a list of names.
func joinNode(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == yaml.SequenceNode {
		return strings.Join(sequenceStrings(n), ", ")
	}
	return scalarString(n)
}

// sequenceStrings flattens a node into a slice of non-empty strings. A scalar
// yields a single element; a sequence yields one element per item.
func sequenceStrings(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if s := strings.TrimSpace(formatNode(c)); s != "" {
				out = append(out, s)
			}
		}
	case yaml.ScalarNode:
		if s := strings.TrimSpace(n.Value); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// formatNode renders an arbitrary YAML node to a compact single-line string for
// display: scalars verbatim, sequences as "a, b, c", and mappings as
// "k: v, k2: v2".
func formatNode(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(n.Value)
	case yaml.SequenceNode:
		parts := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			parts = append(parts, formatNode(c))
		}
		return strings.Join(parts, ", ")
	case yaml.MappingNode:
		parts := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := strings.TrimSpace(n.Content[i].Value)
			v := formatNode(n.Content[i+1])
			parts = append(parts, k+": "+v)
		}
		return strings.Join(parts, ", ")
	case yaml.AliasNode:
		return formatNode(n.Alias)
	default:
		return strings.TrimSpace(n.Value)
	}
}
