// Package pdf renders Markdown to PDF for the GUI "PDF" toolbar button and the
// headless `--pdf` command-line flag.
//
// It auto-selects the best available engine:
//
//   - Chrome/Chromium/Edge (via the DevTools "print to PDF" command) when an
//     installed browser is found. This produces high-fidelity output with
//     selectable, vector text and full feature support (Mermaid diagrams,
//     KaTeX math, syntax highlighting).
//   - goldmark-pdf (pure Go) as a fallback that needs no browser and works
//     completely offline - suitable for headless servers and containers.
//
// All detection is pure Go with no webview linkage, so importing this package
// never adds a native dependency and is safe in headless environments.
package pdf

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// FindBrowser locates an installed Chrome/Chromium/Edge executable that can be
// driven headlessly to print a page to PDF. It returns the executable path and
// true when one is found.
//
// The MDV_CHROME (and the de-facto standard CHROME_BIN) environment variable
// overrides detection, letting users point mdv at a specific browser binary.
func FindBrowser() (string, bool) {
	for _, env := range []string{"MDV_CHROME", "CHROME_BIN"} {
		if p := os.Getenv(env); p != "" {
			if fileExists(p) {
				return p, true
			}
		}
	}
	for _, c := range browserCandidates() {
		if filepath.IsAbs(c) {
			if fileExists(c) {
				return c, true
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, true
		}
	}
	return "", false
}

// browserCandidates returns the platform-specific list of executable names (to
// be resolved via PATH) and absolute install locations to probe, in preference
// order.
func browserCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			filepath.Join(os.Getenv("HOME"), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			filepath.Join(os.Getenv("HOME"), "Applications/Chromium.app/Contents/MacOS/Chromium"),
		}
	case "windows":
		var dirs []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if base := os.Getenv(env); base != "" {
				dirs = append(dirs, base)
			}
		}
		var out []string
		for _, base := range dirs {
			out = append(out,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Chromium\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(base, `BraveSoftware\Brave-Browser\Application\brave.exe`),
			)
		}
		return out
	default: // linux, bsd
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"chrome",
			"microsoft-edge",
			"microsoft-edge-stable",
			"brave-browser",
		}
	}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
