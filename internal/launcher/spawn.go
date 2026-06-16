package launcher

import "errors"

// ErrNoEmbeddedGUI indicates the binary was built without the bundled GUI
// helper (e.g. the console/TUI-only build, or during early development before
// the Wails frontend exists). Callers should fall back to TUI or console.
var ErrNoEmbeddedGUI = errors.New("mdv: GUI helper is not bundled in this build")

// HasEmbeddedGUI reports whether a GUI helper executable is embedded and can be
// spawned. It is wired to the real implementation once the Wails GUI is built
// and embedded; until then it is false so mdv always falls back cleanly.
func HasEmbeddedGUI() bool {
	return embeddedGUIAvailable()
}

// SpawnGUI extracts (if needed) and launches the embedded GUI helper for the
// given input path, returning once the helper has been started. It returns
// ErrNoEmbeddedGUI when no helper is bundled.
func SpawnGUI(inputPath string) error {
	if !HasEmbeddedGUI() {
		return ErrNoEmbeddedGUI
	}
	return spawnGUI(inputPath)
}
