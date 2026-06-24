// Package console renders markdown to ANSI text for non-graphical output. It is
// used when stdout is not a TTY (piped) or when GUI/TUI are unavailable, making
// mdv safe to run in headless containers over SSH.
package console

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/mdfmt"
	"github.com/thgossler/mdv/internal/termimg"
	"golang.org/x/term"
)

// Options control a console render.
type Options struct {
	// Width is the wrap width in columns. Zero means auto-detect (falling back
	// to 80 when stdout is not a terminal).
	Width int
	// MaxWidth caps the wrap width in columns. Zero means no cap.
	MaxWidth int
	// Style is "auto", "dark", "light" or "notty". Empty means auto.
	Style string
	// ShowHeader prints the document path above the rendered content.
	ShowHeader bool
	// ImageMode selects how images are drawn ("auto"|"graphics"|"blocks"|"off").
	// Empty means "auto".
	ImageMode string
	// AllowRemoteImages permits fetching http(s) images.
	AllowRemoteImages bool
}

// RenderFile reads a markdown file and writes rendered ANSI text to w.
func RenderFile(w io.Writer, path string, opt Options) error {
	data, err := core.ReadMarkdownFile(path)
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
	if opt.MaxWidth > 0 && width > opt.MaxWidth {
		width = opt.MaxWidth
	}

	style := opt.Style
	if style == "" {
		style = detectStyle()
	}

	// Emit OSC 8 hyperlinks (clickable links without visible URLs) only on an
	// interactive terminal with color enabled; piped or NO_COLOR output keeps
	// the plain text+URL form so no information is lost.
	hyperlinks := StdoutIsTTY() && os.Getenv("NO_COLOR") == ""

	// Build an image renderer for the document's directory so relative image
	// paths resolve. A ProtocolNone renderer (piped output, unsupported
	// terminal) renders nothing and images keep their alt text.
	var imgRenderer mdfmt.ImageRenderer
	baseDir := "."
	if path != "" {
		baseDir = filepath.Dir(path)
	}
	if proto := termimg.Resolve(termimg.ParseMode(opt.ImageMode), os.Stdout); proto != termimg.ProtocolNone {
		imgRenderer = termimg.NewRenderer(proto, baseDir, opt.AllowRemoteImages)
	}

	out, err := mdfmt.Render(markdown, width, style, hyperlinks, imgRenderer, baseDir)
	if err != nil {
		// Resilience: never abort output entirely on a render failure. Fall
		// back to the raw markdown body (front matter is shown separately
		// below) so the document stays readable, mirroring the TUI's fallback.
		out = core.StripFrontmatter(markdown)
	}

	// Prepend a tidy, unobtrusive metadata block for any leading YAML front
	// matter so it is presented nicely instead of being dropped. mdfmt.Render
	// already strips the raw block from the body.
	fm, _ := core.ExtractFrontmatter(markdown)
	fmBlock := mdfmt.RenderFrontmatter(fm, mdfmt.FrontmatterOptions{
		Width:      width,
		Color:      hyperlinks,
		ShowFields: true,
	})

	if opt.ShowHeader && path != "" {
		fmt.Fprintf(w, "\x1b[2m── %s ──\x1b[0m\n", path)
	}
	if fmBlock != "" {
		if _, err = io.WriteString(w, fmBlock); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, out)
	return err
}

// detectWidth returns the terminal width, or a readable default when stdout is
// not a terminal. The full viewport width is used; callers cap it via
// Options.MaxWidth (the --max-width flag / contentWidth config) when desired.
func detectWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
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
