package core

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ResolveLink classifies and resolves a raw markdown link href against the
// directory of the currently open document. workspace may be nil; when present
// it is used to resolve bare wikilink-style targets.
//
// The resolver is deliberately tolerant: it URL-decodes paths, tries adding a
// markdown extension when a file is missing, and degrades to LinkBroken rather
// than erroring.
func ResolveLink(raw, currentDir, rootDir string, cfg Defaults, workspace []DocFile) LinkTarget {
	href := strings.TrimSpace(raw)
	t := LinkTarget{Raw: raw, Display: raw}

	if href == "" {
		t.Kind = LinkUnknown
		return t
	}

	// Wikilink inner text, e.g. when the caller already stripped [[ ]].
	if strings.HasPrefix(href, "[[") && strings.HasSuffix(href, "]]") {
		return ResolveWikilink(href[2:len(href)-2], cfg, workspace)
	}

	// Pure in-document anchor.
	if strings.HasPrefix(href, "#") {
		frag := strings.TrimPrefix(href, "#")
		t.Kind = LinkAnchor
		t.Resolved = "#" + frag
		t.Fragment = frag
		t.Display = href
		return t
	}

	// Scheme-based URLs.
	if scheme, ok := schemeOf(href); ok {
		switch scheme {
		case "http", "https":
			t.Kind = LinkHTTP
			t.Resolved = href
			t.Display = href
		case "mailto":
			t.Kind = LinkMailto
			t.Resolved = href
			t.Display = href
		case "file":
			if u, err := url.Parse(href); err == nil {
				return resolveFilePath(u.Path, "", currentDir, rootDir, cfg, raw)
			}
			t.Kind = LinkUnknown
		default:
			// ftp, vscode, custom schemes: let the OS handle it.
			t.Kind = LinkHTTP
			t.Resolved = href
			t.Display = href
		}
		return t
	}

	// Protocol-relative URL ("//host/path").
	if strings.HasPrefix(href, "//") {
		t.Kind = LinkHTTP
		t.Resolved = "https:" + href
		t.Display = t.Resolved
		return t
	}

	// Local path, possibly with a #fragment.
	path, frag := splitFragment(href)
	return resolveFilePath(path, frag, currentDir, rootDir, cfg, raw)
}

// resolveFilePath resolves a (possibly relative) filesystem path to a markdown,
// external-file, or broken target.
//
// rootDir is the workspace root. A root-relative link (one starting with "/",
// as produced by Azure DevOps wiki) is first tried as a literal absolute
// filesystem path; if nothing is found there, it is resolved relative to the
// workspace root instead. rootDir may be empty to disable that fallback.
func resolveFilePath(path, frag, currentDir, rootDir string, cfg Defaults, raw string) LinkTarget {
	t := LinkTarget{Raw: raw, Fragment: frag, Display: raw}

	decoded := decodePath(path)
	decoded = expandHome(decoded)
	decoded = filepath.FromSlash(decoded)

	// Anchor-only after splitting (e.g. "#foo" already handled, but "./#foo").
	if decoded == "" && frag != "" {
		t.Kind = LinkAnchor
		t.Resolved = "#" + frag
		t.Display = "#" + frag
		return t
	}

	rootRelative := filepath.IsAbs(decoded)
	abs := decoded
	if !rootRelative {
		abs = filepath.Join(currentDir, decoded)
	}
	abs = filepath.Clean(abs)

	resolved, isMD, exists := probePath(abs, cfg)

	// Root-relative link that is missing on disk: retry against the workspace
	// root (Azure DevOps wiki style, e.g. "/index.md").
	if !exists && rootRelative && rootDir != "" {
		rel := strings.TrimPrefix(decoded, string(os.PathSeparator))
		rooted := filepath.Clean(filepath.Join(rootDir, rel))
		if r2, md2, ex2 := probePath(rooted, cfg); ex2 {
			resolved, isMD, exists = r2, md2, ex2
		}
	}

	t.Resolved = resolved
	t.Display = resolved
	if frag != "" {
		t.Display = resolved + "#" + frag
	}

	switch {
	case !exists:
		t.Kind = LinkBroken
	case isMD:
		t.Kind = LinkMarkdown
	default:
		t.Kind = LinkExternalFile
	}
	return t
}

// probePath checks whether abs exists, trying a markdown extension and an
// index/README fallback for directories. It returns the resolved path, whether
// it is markdown, and whether something was found.
func probePath(abs string, cfg Defaults) (resolved string, isMD bool, exists bool) {
	if info, err := os.Stat(abs); err == nil {
		if info.IsDir() {
			// Directory link: prefer README.md / index.md inside it.
			for _, cand := range []string{"README.md", "readme.md", "index.md", "README.markdown"} {
				p := filepath.Join(abs, cand)
				if _, err := os.Stat(p); err == nil {
					return p, true, true
				}
			}
			return abs, false, true
		}
		return abs, IsMarkdownPath(abs, cfg), true
	}

	// File missing: if it has no extension, try the markdown ones.
	if filepath.Ext(abs) == "" {
		for _, ext := range cfg.MarkdownExtensions {
			p := abs + ext
			if _, err := os.Stat(p); err == nil {
				return p, true, true
			}
		}
	}

	return abs, IsMarkdownPath(abs, cfg), false
}

// ResolveWikilink resolves [[note]], [[note|alias]] and [[note#heading]] against
// the workspace. A heading-only wikilink ([[#heading]]) becomes an anchor.
func ResolveWikilink(inner string, cfg Defaults, workspace []DocFile) LinkTarget {
	raw := "[[" + inner + "]]"
	t := LinkTarget{Raw: raw, Display: raw}

	target := inner
	if i := strings.Index(target, "|"); i >= 0 {
		target = target[:i] // drop alias for resolution
	}
	target = strings.TrimSpace(target)

	name, frag := splitFragment(target)
	name = strings.TrimSpace(name)
	t.Fragment = frag

	if name == "" {
		// [[#heading]] -> in-document anchor.
		t.Kind = LinkAnchor
		t.Resolved = "#" + BaseSlug(frag)
		t.Display = "#" + frag
		return t
	}

	if doc, ok := findWorkspaceDoc(name, cfg, workspace); ok {
		t.Kind = LinkWikiInternal
		t.Resolved = doc.Path
		t.Display = doc.Name
		if frag != "" {
			t.Display = doc.Name + "#" + frag
		}
		return t
	}

	t.Kind = LinkBroken
	t.Resolved = name
	return t
}

// findWorkspaceDoc matches a wikilink target against workspace files by base
// name (case-insensitive, with or without extension), then by relative path,
// then by document title.
func findWorkspaceDoc(target string, cfg Defaults, workspace []DocFile) (DocFile, bool) {
	if len(workspace) == 0 {
		return DocFile{}, false
	}
	norm := strings.ToLower(strings.TrimSpace(target))
	normSlash := filepath.ToSlash(norm)

	// 1. Exact base name with extension.
	for _, d := range workspace {
		if strings.ToLower(d.Name) == norm {
			return d, true
		}
	}
	// 2. Base name without extension.
	for _, d := range workspace {
		stem := strings.ToLower(strings.TrimSuffix(d.Name, filepath.Ext(d.Name)))
		if stem == norm {
			return d, true
		}
	}
	// 3. Path suffix match (e.g. "docs/setup").
	for _, d := range workspace {
		p := strings.ToLower(filepath.ToSlash(d.Path))
		stem := strings.TrimSuffix(p, strings.ToLower(filepath.Ext(d.Path)))
		if strings.HasSuffix(stem, normSlash) || strings.HasSuffix(p, normSlash) {
			return d, true
		}
	}
	// 4. Title match.
	for _, d := range workspace {
		if d.Title != "" && strings.EqualFold(d.Title, target) {
			return d, true
		}
	}
	return DocFile{}, false
}

// schemeOf extracts a URL scheme like "https" from "https://...". It only
// recognises RFC-3986-style schemes followed by ':'.
func schemeOf(s string) (string, bool) {
	i := strings.Index(s, ":")
	if i <= 0 {
		return "", false
	}
	scheme := s[:i]
	for j, r := range scheme {
		isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if j == 0 && !isAlpha {
			return "", false
		}
		if !isAlpha && !isDigit && r != '+' && r != '-' && r != '.' {
			return "", false
		}
	}
	// Avoid treating Windows drive letters ("C:\...") as schemes.
	if len(scheme) == 1 {
		return "", false
	}
	return strings.ToLower(scheme), true
}

// splitFragment splits "path#frag" into ("path", "frag").
func splitFragment(s string) (string, string) {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// decodePath percent-decodes a path, falling back to the original on error.
func decodePath(p string) string {
	if !strings.Contains(p, "%") {
		return p
	}
	if dec, err := url.PathUnescape(p); err == nil {
		return dec
	}
	return p
}
