// Package launcher decides how mdv should present a document — GUI, TUI or
// plain console — and (later) extracts and spawns the embedded GUI helper.
//
// All detection here is pure Go with no webview linkage, so importing this
// package never adds a native library dependency. That is what allows the
// shipped binary to start in a headless container that lacks WebKitGTK.
package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/thomasgossler/mdv/internal/console"
)

// Mode is a chosen presentation mode.
type Mode int

const (
	// ModeConsole dumps rendered markdown to stdout and exits.
	ModeConsole Mode = iota
	// ModeTUI runs the interactive terminal UI.
	ModeTUI
	// ModeGUI runs the windowed webview UI.
	ModeGUI
)

func (m Mode) String() string {
	switch m {
	case ModeTUI:
		return "tui"
	case ModeGUI:
		return "gui"
	default:
		return "console"
	}
}

// Preference expresses the user's explicit mode request from CLI flags.
type Preference struct {
	ForceGUI     bool // --gui
	ForceTUI     bool // --tui
	ForceConsole bool // --console / -c
}

// DetectMode resolves the effective presentation mode from explicit user
// preference and the runtime environment.
//
// Decision order:
//  1. Explicit flags win (with console fallback if a forced GUI is impossible).
//  2. Non-interactive stdout (piped/redirected) => console dump.
//  3. A usable GUI environment => GUI.
//  4. An interactive terminal => TUI.
//  5. Otherwise => console.
func DetectMode(pref Preference) Mode {
	switch {
	case pref.ForceConsole:
		return ModeConsole
	case pref.ForceTUI:
		return ModeTUI
	case pref.ForceGUI:
		if GUIAvailable() {
			return ModeGUI
		}
		// Asked for GUI but it cannot run: degrade gracefully.
		if console.StdoutIsTTY() {
			return ModeTUI
		}
		return ModeConsole
	}

	// No explicit preference: infer.
	if !console.StdoutIsTTY() {
		return ModeConsole
	}
	if GUIAvailable() {
		return ModeGUI
	}
	if console.StdinIsTTY() {
		return ModeTUI
	}
	return ModeConsole
}

// GUIAvailable reports whether a windowed webview can plausibly be started on
// this machine. It is conservative on Linux (requires both a display server and
// the WebKitGTK shared library) so we never spawn a helper that would crash.
func GUIAvailable() bool {
	switch runtime.GOOS {
	case "darwin":
		return macGUIAvailable()
	case "windows":
		return windowsGUIAvailable()
	default:
		return linuxGUIAvailable()
	}
}

// macGUIAvailable returns false only for obvious headless cases. macOS provides
// WKWebView in the OS, so a GUI is available whenever there is a window server.
func macGUIAvailable() bool {
	// A pure SSH session with no Aqua session generally cannot show windows.
	if os.Getenv("SSH_CONNECTION") != "" && os.Getenv("__CFBundleIdentifier") == "" {
		// Best-effort: assume no GUI under bare SSH. The spawn step still
		// falls back if this is wrong in either direction.
		return false
	}
	return true
}

// windowsGUIAvailable checks for the WebView2 runtime. The Evergreen runtime is
// present by default on Windows 11; if absent, mdv falls back to TUI/console.
func windowsGUIAvailable() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	// Common Evergreen install locations (per-machine and per-user).
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "EdgeWebView", "Application"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "EdgeWebView", "Application"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "EdgeWebView", "Application"),
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	// Could not confirm; allow the attempt (spawn fallback covers failure).
	return true
}

// linuxGUIAvailable requires a display server and the WebKitGTK library.
func linuxGUIAvailable() bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return webkitGTKPresent()
}

// webkitGTKPresent scans the usual library directories for a libwebkit2gtk
// shared object (4.1/6.0/4.0). It avoids dlopen so no cgo or linkage is needed.
func webkitGTKPresent() bool {
	names := []string{
		"libwebkit2gtk-4.1.so",
		"libwebkit2gtk-4.0.so",
		"libwebkitgtk-6.0.so",
		"libwebkit2gtk-4.1.so.0",
		"libwebkit2gtk-4.0.so.37",
	}
	dirs := []string{
		"/usr/lib",
		"/usr/lib64",
		"/usr/local/lib",
		"/lib",
		"/usr/lib/x86_64-linux-gnu",
		"/usr/lib/aarch64-linux-gnu",
	}
	if extra := os.Getenv("LD_LIBRARY_PATH"); extra != "" {
		dirs = append(dirs, strings.Split(extra, ":")...)
	}

	for _, dir := range dirs {
		for _, name := range names {
			if fileExists(filepath.Join(dir, name)) {
				return true
			}
		}
		// Also match versioned suffixes via a shallow scan.
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				n := e.Name()
				if strings.HasPrefix(n, "libwebkit2gtk-4.") || strings.HasPrefix(n, "libwebkitgtk-6.") {
					return true
				}
			}
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
