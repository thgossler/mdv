//go:build gui_bundled && !darwin

package launcher

// finalizeHelper is a no-op on non-macOS platforms: the extracted helper is run
// directly. The macOS build wraps it in an .app bundle so the application icon
// shows in the Dock and Cmd+Tab switcher.
func finalizeHelper(exe string) (string, error) {
	return exe, nil
}
