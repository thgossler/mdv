// Package launcher decides how mdv should present a document - GUI, TUI or
// plain console - and (later) extracts and spawns the embedded GUI helper.
//
// All detection here is pure Go with no webview linkage, so importing this
// package never adds a native library dependency. That is what allows the
// shipped binary to start in a headless container that lacks WebKitGTK.
package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/thgossler/mdv/internal/console"
)

// minGTK4Minor is the lowest GTK 4 minor version the bundled GUI helper can run
// against. The helper is built (via Wails v3) on the release runners, which ship
// GTK 4.14, and it calls APIs introduced in that release - e.g.
// gdk_monitor_get_scale (since 4.14) and the GtkFileDialog family (since 4.10).
// On an older GTK those symbols are missing, so the helper aborts on startup.
// Detecting the version here lets mdv degrade to TUI/console instead of
// spawning a helper that silently dies.
const minGTK4Minor = 14

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
		// Explorer / Spotlight launch: no console and no redirection anywhere.
		// There is nowhere to dump text, so go straight to the GUI.
		if !console.HasStartupConsole() && GUIAvailable() {
			return ModeGUI
		}
		// Windows GUI-subsystem binary launched from a terminal: AttachConsole
		// reattaches the parent console but the resulting CONOUT$ handle may not
		// pass term.IsTerminal, so StdoutIsTTY() is unreliable here. Use the
		// explicit attachment flag instead to detect an interactive session.
		if console.IsAttachedToTerminal() {
			if GUIAvailable() {
				return ModeGUI
			}
			return ModeTUI
		}
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

// linuxGUIAvailable requires a display server, a GTK 4 runtime new enough for
// the bundled helper, and the WebKitGTK library. The GTK version gate ensures we
// never spawn a helper that would crash on missing symbols (older GTK), so mdv
// degrades cleanly to the TUI/console instead.
func linuxGUIAvailable() bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	if !gtk4AtLeast(minGTK4Minor) {
		return false
	}
	return webkitGTKPresent()
}

// gtk4AtLeast reports whether an installed GTK 4 runtime is at least version
// 4.<minMinor>. When no GTK 4 library can be found or its version parsed, it
// returns false so mdv stays on the safe TUI/console path.
func gtk4AtLeast(minMinor int) bool {
	minor, ok := gtk4MinorInDirs(libDirs())
	return ok && minor >= minMinor
}

// gtk4MinorInDirs scans the given directories for the GTK 4 shared object and
// returns the highest GTK minor version it can derive, without dlopen or cgo.
// GTK ships the library as a versioned file named
// libgtk-4.so.1.<minor*100>.<micro> (e.g. libgtk-4.so.1.1400.2 for 4.14.2), and
// the bare soname libgtk-4.so.1 is a symlink to it. Both are inspected.
func gtk4MinorInDirs(dirs []string) (int, bool) {
	best := -1
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if m, ok := parseGTK4Minor(name); ok && m > best {
				best = m
			}
			// The bare soname symlink points at the versioned object; follow it
			// so systems that expose only libgtk-4.so.1 are still detected.
			if name == "libgtk-4.so.1" {
				if target, err := os.Readlink(filepath.Join(dir, name)); err == nil {
					if m, ok := parseGTK4Minor(filepath.Base(target)); ok && m > best {
						best = m
					}
				}
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// parseGTK4Minor extracts the GTK minor version from a versioned GTK 4 library
// filename of the form libgtk-4.so.1.<minor*100>.<micro>. For example
// "libgtk-4.so.1.1400.2" yields 14 and "libgtk-4.so.1.800.3" yields 8.
func parseGTK4Minor(name string) (int, bool) {
	const prefix = "libgtk-4.so.1."
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	rest := name[len(prefix):] // e.g. "1400.2"
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 {
		return 0, false
	}
	enc, err := strconv.Atoi(rest[:dot])
	if err != nil || enc < 0 {
		return 0, false
	}
	return enc / 100, true
}

// libDirs returns the usual shared-library search directories, plus any from
// LD_LIBRARY_PATH. Used for cgo-free probing of GTK/WebKitGTK presence.
func libDirs() []string {
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
	return dirs
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
	dirs := libDirs()

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
