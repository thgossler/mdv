package tui

import "regexp"

// regexpMustCompileANSI builds a regex that matches ANSI escape sequences so
// rendered output can be searched and scrolled by plain text.
func regexpMustCompileANSI() *regexp.Regexp {
	return regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
}
