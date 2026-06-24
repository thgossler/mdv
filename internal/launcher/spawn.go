package launcher

import (
	"errors"
	"os"
)

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

// MDVPickEnv is the environment variable the launcher sets to tell the spawned
// GUI helper to present an "open file or folder" picker instead of loading a
// path. It is used when mdv is started with no input but a GUI will be shown
// (e.g. double-clicked from Finder/Explorer).
const MDVPickEnv = "MDV_PICK"

// MDVRemoteEnv is the environment variable the launcher sets when mdv is started
// with --remote, telling the spawned GUI helper to begin the session with
// remote (http/https) image loading enabled (the toolbar toggle starts active).
const MDVRemoteEnv = "MDV_REMOTE"

// MDVIgnoreEnv is the environment variable the launcher sets when mdv is started
// with --ignore, telling the spawned GUI helper to use the supplied patterns as
// the initial navigator exclusion list for this session without persisting them.
const MDVIgnoreEnv = "MDV_IGNORE"

// MDVSidePanelEnv is the environment variable the launcher sets when mdv is
// started with --sidepanel, telling the spawned GUI helper to keep the document
// navigator panel visible for this session even when a single file is opened.
const MDVSidePanelEnv = "MDV_SIDEPANEL"

// SpawnGUIPicker launches the embedded GUI helper with no input path, signalling
// it (via MDVPickEnv) to present a native file/folder picker on startup. It
// returns ErrNoEmbeddedGUI when no helper is bundled. The environment variable
// is inherited by the detached child process.
func SpawnGUIPicker() error {
	if !HasEmbeddedGUI() {
		return ErrNoEmbeddedGUI
	}
	if err := os.Setenv(MDVPickEnv, "1"); err != nil {
		return err
	}
	return spawnGUI("")
}
