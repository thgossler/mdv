package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileAssocSchemeVersion is the version of mdv's OS file-manager integration -
// the "Open with mdv" entry shown for Markdown files (a context-menu verb on
// Windows, the Finder "Open With" submenu on macOS). Bump it whenever the
// registration scheme changes (e.g. a new extension or a different command) so
// existing installs re-register on their next launch.
const FileAssocSchemeVersion = 1

// fileAssocStateKey is the state.jsonc key that records the scheme version last
// registered, so registration runs once rather than on every startup.
const fileAssocStateKey = "fileAssocVersion"

// markdownExtensions are the Markdown file types mdv registers an "Open with
// mdv" entry for.
var markdownExtensions = []string{".md", ".mdx", ".markdown"}

// stateHeader mirrors the GUI's state.jsonc header so the file reads as JSONC
// regardless of which component wrote it last.
const stateHeader = "// mdv window & panel layout - managed automatically, safe to delete.\n"

// statePath returns the path to state.jsonc, shared with the GUI's layout state.
func statePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.jsonc"), nil
}

// EnsureFileAssociations registers an "Open with mdv" entry in the OS file
// manager for Markdown files, unless a previous run already did so for the
// current scheme version (recorded in state.jsonc for fast startup). It is
// best-effort: a returned error should be surfaced as a warning, not treated as
// fatal. Unsupported platforms - and layouts where mdv cannot be registered,
// such as a bare binary install on macOS - are silent no-ops.
func EnsureFileAssociations() error {
	if fileAssocVersionFromState() >= FileAssocSchemeVersion {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := registerFileAssociations(exe); err != nil {
		return err
	}
	return setFileAssocVersion(FileAssocSchemeVersion)
}

// fileAssocVersionFromState reads the recorded registration scheme version from
// state.jsonc, returning 0 when the file is absent, unreadable, or has no value.
func fileAssocVersionFromState() int {
	path, err := statePath()
	if err != nil {
		return 0
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(StripJSONC(raw), &m) != nil {
		return 0
	}
	v, ok := m[fileAssocStateKey]
	if !ok {
		return 0
	}
	var n int
	if json.Unmarshal(v, &n) != nil {
		return 0
	}
	return n
}

// setFileAssocVersion records the registered scheme version in state.jsonc,
// preserving every other key already present (e.g. the GUI's window geometry)
// so the two writers never clobber each other's data.
func setFileAssocVersion(version int) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	m := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(StripJSONC(raw), &m)
	}
	vb, err := json.Marshal(version)
	if err != nil {
		return err
	}
	m[fileAssocStateKey] = vb
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(stateHeader), body...), 0o644)
}
