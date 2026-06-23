//go:build gui_bundled

package launcher

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thgossler/mdv/internal/core"
)

// assets holds the compressed GUI helper executable. The build pipeline writes
// the platform-specific helper to internal/launcher/assets/mdv-gui.gz before
// compiling with `-tags gui_bundled`. Only a .gitkeep is committed, so when the
// helper has not been produced the ReadFile below simply fails and the launcher
// falls back to TUI/console.
//
//go:embed all:assets
var assets embed.FS

// helperName is the on-disk name of the extracted helper. It is deliberately
// "mdv" (not "mdv-gui"): on macOS an unbundled binary's process name is what
// AppKit shows as the bold application name in the menu bar, and on
// Windows/Linux it is the process image name. Naming it "mdv" makes the running
// GUI present itself as "mdv" everywhere. The launcher and helper live in
// separate directories, so the shared basename never collides on disk.
func helperName() string {
	if runtime.GOOS == "windows" {
		return "mdv.exe"
	}
	return "mdv"
}

// embeddedGUIAvailable reports whether a usable (non-placeholder) GUI helper is
// bundled in this build.
func embeddedGUIAvailable() bool {
	data, err := assets.ReadFile("assets/mdv-gui.gz")
	if err != nil {
		return false
	}
	// The placeholder is a tiny sentinel; a real helper is multiple megabytes.
	return len(data) > 4096
}

// spawnGUI extracts the embedded helper to a versioned cache directory (only
// when missing or stale) and launches it detached for the given input path.
func spawnGUI(inputPath string) error {
	exe, err := extractHelper()
	if err != nil {
		return err
	}
	args := []string{}
	if inputPath != "" {
		args = append(args, inputPath)
	}
	return core.SpawnDetached(exe, args...)
}

// extractHelper writes the embedded helper to the cache dir if absent or if its
// checksum differs, then returns the executable path. Extraction is keyed by
// the app version so upgrades replace the helper automatically.
func extractHelper() (string, error) {
	gz, err := assets.ReadFile("assets/mdv-gui.gz")
	if err != nil {
		return "", ErrNoEmbeddedGUI
	}
	if len(gz) <= 4096 {
		return "", ErrNoEmbeddedGUI
	}

	raw, err := gunzip(gz)
	if err != nil {
		return "", err
	}

	dir, err := helperCacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, helperName())

	if !fileMatches(out, raw) {
		tmp := out + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, out); err != nil {
			return "", err
		}
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(out, 0o755)
	}
	return out, nil
}

// helperCacheDir is a per-version directory under the user cache, so multiple
// installed versions do not clobber one another.
func helperCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, core.AppName, "gui", core.Version), nil
}

// fileMatches reports whether the file at path already equals want by SHA-256.
func fileMatches(path string, want []byte) bool {
	have, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	hHave := sha256.Sum256(have)
	hWant := sha256.Sum256(want)
	return hex.EncodeToString(hHave[:]) == hex.EncodeToString(hWant[:])
}

// gunzip decompresses a gzip-compressed byte slice.
func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("mdv: empty GUI helper")
	}
	return out, nil
}
