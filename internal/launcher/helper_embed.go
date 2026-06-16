//go:build !gui_bundled

package launcher

// This file provides the default (no bundled GUI) implementation. When the
// Wails GUI helper is built, a build-tagged variant embeds the helper binary
// via go:embed, extracts it to the user cache directory, and execs it. Keeping
// the default here ensures the launcher always compiles and links with zero
// native/webview dependencies — the property that lets mdv start in headless
// containers.

// embeddedGUIAvailable reports whether a GUI helper is bundled.
func embeddedGUIAvailable() bool { return false }

// spawnGUI launches the bundled GUI helper. The stub always reports that no
// helper is bundled so callers fall back to TUI/console.
func spawnGUI(inputPath string) error { return ErrNoEmbeddedGUI }
