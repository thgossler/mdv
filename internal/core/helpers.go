package core

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Shared regexes for link scanning (backlinks, etc.). Group 1 of reInlineLink
// captures the link destination (href).
var (
	reInlineLink = regexp.MustCompile(`\[[^\]]*\]\(\s*([^)]+?)\s*\)`)
	reWikiLink   = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

// cleanPath returns an absolute, slash-cleaned form of p for comparison.
func cleanPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(p)
}

// dirOf returns the directory containing path.
func dirOf(path string) string { return filepath.Dir(path) }

// baseStem returns the file name without its extension.
func baseStem(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
