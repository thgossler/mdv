// Package console renders markdown to ANSI text for non-graphical output. It is
// used when stdout is not a TTY (piped) or when GUI/TUI are unavailable, making
// mdv safe to run in headless containers over SSH.
package console

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// Options control a console render.
type Options struct {
	// Width is the wrap width in columns. Zero means auto-detect (falling back
	// to 80 when stdout is not a terminal).
	Width int
	// Style is "auto", "dark", "light" or "notty". Empty means auto.
	Style string
	// ShowHeader prints the document path above the rendered content.
	ShowHeader bool
}

// RenderFile reads a markdown file and writes rendered ANSI text to w.
func RenderFile(w io.Writer, path string, opt Options) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Render(w, string(data), path, opt)
}

// Render writes rendered markdown to w. The path is used only for the optional
// header.
func Render(w io.Writer, markdown, path string, opt Options) error {
	width := opt.Width
	if width <= 0 {
		width = detectWidth()
	}

	style := opt.Style
	if style == "" {
		style = detectStyle()
	}

	r, err := newRenderer(style, width)
	if err != nil {
		return err
	}

	out, err := r.Render(markdown)
	if err != nil {
		return err
	}

	if opt.ShowHeader && path != "" {
		fmt.Fprintf(w, "\x1b[2m── %s ──\x1b[0m\n", path)
	}
	_, err = io.WriteString(w, out)
	return err
}

func newRenderer(style string, width int) (*glamour.TermRenderer, error) {
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

// detectWidth returns the terminal width, clamped to a readable range.
func detectWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		if w > 120 {
			return 120
		}
		return w
	}
	return 80
}

// detectStyle chooses a glamour style honoring NO_COLOR and TTY state.
func detectStyle() string {
	if os.Getenv("NO_COLOR") != "" {
		return "notty"
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return "notty"
	}
	return "auto"
}

// StdoutIsTTY reports whether standard output is an interactive terminal.
func StdoutIsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// StdinIsTTY reports whether standard input is an interactive terminal.
func StdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
