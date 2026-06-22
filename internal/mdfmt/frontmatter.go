package mdfmt

import (
	"strings"

	"github.com/thgossler/mdv/internal/core"
)

// ANSI SGR codes used for the unobtrusive front matter block. Kept as plain
// escapes (rather than lipgloss) so the same output works for both the console
// and the TUI viewport.
const (
	sgrReset = "\x1b[0m"
	sgrBold  = "\x1b[1m"
	sgrDim   = "\x1b[2m"
)

// FrontmatterOptions control how RenderFrontmatter formats a metadata block.
type FrontmatterOptions struct {
	// Width is the column width used for the trailing separator rule. Values
	// below 1 fall back to a short default.
	Width int
	// Color enables ANSI styling. When false, a plain-text block is produced
	// (honoring NO_COLOR / non-TTY output).
	Color bool
	// ShowFields includes the remaining (non-headline) fields. The TUI sets
	// this false when the metadata section is collapsed.
	ShowFields bool
	// Hint, when non-empty, is shown as a faint line just above the separator
	// rule (used by the TUI to advertise the expand/collapse key).
	Hint string
}

// RenderFrontmatter formats parsed front matter into an unobtrusive block to be
// shown at the top of a rendered document. The headline fields (title, author,
// date, tags) are always included; remaining fields are included only when
// opt.ShowFields is true. The result ends with a faint separator rule and a
// trailing blank line, or is empty when there is nothing to show.
func RenderFrontmatter(fm core.Frontmatter, opt FrontmatterOptions) string {
	if !fm.Has {
		return ""
	}

	width := opt.Width
	if width < 1 {
		width = 40
	}
	if width > 120 {
		width = 120
	}

	bold := func(s string) string {
		if opt.Color {
			return sgrBold + s + sgrReset
		}
		return s
	}
	dim := func(s string) string {
		if opt.Color {
			return sgrDim + s + sgrReset
		}
		return s
	}

	var lines []string

	if fm.Title != "" {
		lines = append(lines, bold(fm.Title))
	}

	meta := make([]string, 0, 2)
	if fm.Author != "" {
		meta = append(meta, fm.Author)
	}
	if fm.Date != "" {
		meta = append(meta, fm.Date)
	}
	if len(meta) > 0 {
		lines = append(lines, dim(strings.Join(meta, " · ")))
	}

	if len(fm.Tags) > 0 {
		tags := make([]string, len(fm.Tags))
		for i, t := range fm.Tags {
			tags[i] = "#" + t
		}
		lines = append(lines, dim(strings.Join(tags, "  ")))
	}

	if opt.ShowFields && len(fm.Fields) > 0 {
		keyW := 0
		for _, f := range fm.Fields {
			if n := len([]rune(f.Key)); n > keyW {
				keyW = n
			}
		}
		for _, f := range fm.Fields {
			pad := strings.Repeat(" ", keyW-len([]rune(f.Key)))
			lines = append(lines, dim(f.Key+pad+"   "+f.Value))
		}
	}

	if len(lines) == 0 {
		return ""
	}

	if opt.Hint != "" {
		lines = append(lines, dim(opt.Hint))
	}

	lines = append(lines, dim(strings.Repeat("─", width)))
	return strings.Join(lines, "\n") + "\n\n"
}

// FrontmatterHiddenCount reports how many non-headline fields exist, so an
// interactive surface can show a hint such as "N more fields".
func FrontmatterHiddenCount(fm core.Frontmatter) int {
	return len(fm.Fields)
}
