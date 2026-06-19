// Package termimg renders images inside terminals. It detects the best
// available inline-image protocol (kitty, iTerm2, or sixel) and falls back to
// Unicode half-block "pixels" on any truecolor/256-color terminal, and finally
// to nothing (callers then keep the image's alt text).
//
// The package is split across files:
//
//   - termimg.go  - capability detection and the public Protocol type
//   - load.go     - decoding image bytes (PNG/JPEG/GIF/WebP/SVG) from files,
//     data: URIs, and remote URLs
//   - encode.go   - encoding a decoded image for a given protocol
//   - markdown.go - replacing standalone markdown images with rendered output
package termimg

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// Protocol identifies how images are drawn in a terminal.
type Protocol int

const (
	// ProtocolNone means no image rendering is possible; callers fall back to
	// the image's alt text.
	ProtocolNone Protocol = iota
	// ProtocolBlocks renders a low-resolution image from Unicode upper-half
	// block glyphs with truecolor foreground/background. It composes with any
	// color terminal and, crucially, with cell-diffing TUIs (Bubble Tea),
	// because the result is just colored text.
	ProtocolBlocks
	// ProtocolSixel uses the DEC sixel bitmap protocol.
	ProtocolSixel
	// ProtocolITerm2 uses the iTerm2 inline-image protocol (OSC 1337).
	ProtocolITerm2
	// ProtocolKitty uses the kitty graphics protocol.
	ProtocolKitty
)

// String returns a short identifier, primarily for diagnostics and config.
func (p Protocol) String() string {
	switch p {
	case ProtocolBlocks:
		return "blocks"
	case ProtocolSixel:
		return "sixel"
	case ProtocolITerm2:
		return "iterm2"
	case ProtocolKitty:
		return "kitty"
	default:
		return "none"
	}
}

// Mode is the user-facing image preference parsed from configuration.
type Mode int

const (
	// ModeAuto picks the best available method for the terminal.
	ModeAuto Mode = iota
	// ModeGraphics forces a pixel protocol (kitty/iTerm2/sixel) and disables the
	// half-block fallback, drawing nothing where no pixel protocol exists.
	ModeGraphics
	// ModeBlocks forces the Unicode half-block renderer.
	ModeBlocks
	// ModeOff disables image rendering entirely (alt text only).
	ModeOff
)

// ParseMode converts a configuration string into a Mode. Unknown values map to
// ModeAuto so a typo never disables a perfectly capable terminal silently.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "none", "false", "0":
		return ModeOff
	case "blocks", "block", "ascii", "text-pixels":
		return ModeBlocks
	case "graphics", "pixel", "pixels", "inline":
		return ModeGraphics
	default:
		return ModeAuto
	}
}

// DetectGraphics returns the best inline pixel protocol supported by the
// terminal attached to f, or ProtocolNone if none is detected. Detection is
// based on environment variables only; it never writes to or reads from the
// terminal, so it is safe to call before a TUI takes over the screen.
//
// Inside tmux or GNU screen the inline protocols are disabled: their passthrough
// is fragile and varies by configuration, so callers should fall back to
// half-blocks there.
func DetectGraphics(f *os.File) Protocol {
	if f == nil || !term.IsTerminal(int(f.Fd())) {
		return ProtocolNone
	}
	if insideMultiplexer() {
		return ProtocolNone
	}

	termEnv := os.Getenv("TERM")
	termProgram := os.Getenv("TERM_PROGRAM")
	lcTerminal := os.Getenv("LC_TERMINAL")

	// kitty and Ghostty: most capable, prefer them.
	if os.Getenv("KITTY_WINDOW_ID") != "" ||
		strings.Contains(termEnv, "kitty") ||
		strings.Contains(termEnv, "ghostty") ||
		termProgram == "ghostty" {
		return ProtocolKitty
	}

	// iTerm2-protocol terminals (iTerm2 itself, WezTerm, mintty, rio).
	if termProgram == "iTerm.app" || lcTerminal == "iTerm2" ||
		termProgram == "WezTerm" || os.Getenv("WEZTERM_PANE") != "" ||
		termProgram == "rio" || strings.Contains(termEnv, "rio") ||
		strings.Contains(termEnv, "mintty") {
		return ProtocolITerm2
	}

	// Terminals known to implement sixel and not the protocols above.
	if strings.Contains(termEnv, "sixel") ||
		strings.Contains(termEnv, "mlterm") ||
		strings.Contains(termEnv, "foot") ||
		strings.Contains(termEnv, "yaft") ||
		strings.Contains(termEnv, "st-") {
		return ProtocolSixel
	}

	return ProtocolNone
}

// SupportsColor reports whether f is a terminal that can show the half-block
// renderer, honoring the NO_COLOR convention.
func SupportsColor(f *os.File) bool {
	if f == nil || !term.IsTerminal(int(f.Fd())) {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

// Resolve picks the protocol to use for output file f given the user's mode. It
// returns ProtocolNone when images should not be drawn.
func Resolve(mode Mode, f *os.File) Protocol {
	switch mode {
	case ModeOff:
		return ProtocolNone
	case ModeBlocks:
		if SupportsColor(f) {
			return ProtocolBlocks
		}
		return ProtocolNone
	case ModeGraphics:
		return DetectGraphics(f)
	default: // ModeAuto
		if g := DetectGraphics(f); g != ProtocolNone {
			return g
		}
		if SupportsColor(f) {
			return ProtocolBlocks
		}
		return ProtocolNone
	}
}

// insideMultiplexer reports whether we appear to be running inside tmux or GNU
// screen, where inline image protocols are unreliable.
func insideMultiplexer() bool {
	if os.Getenv("TMUX") != "" {
		return true
	}
	if t := os.Getenv("TERM"); strings.HasPrefix(t, "screen") || strings.HasPrefix(t, "tmux") {
		return true
	}
	return false
}
