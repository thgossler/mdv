package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// viewerStateHeader is prepended to state.jsonc so it reads as JSONC, matching
// what the GUI writes. Go's encoding/json cannot emit comments, so the body is
// plain (valid) JSON.
const viewerStateHeader = "// mdv window & panel layout - managed automatically, safe to delete.\n"

// stateKeyExtendedSyntax is the state.jsonc key holding the user's runtime
// choice for the "extended" Markdown syntax toggle.
const stateKeyExtendedSyntax = "extendedSyntax"

// ViewerStatePath returns the full path to state.jsonc, the file that stores
// runtime UI preferences shared between the GUI and the terminal UI (window
// layout is owned by the GUI; the extended-syntax toggle is shared).
func ViewerStatePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.jsonc"), nil
}

// readViewerState loads state.jsonc into a generic map so individual keys can be
// updated without dropping fields owned by another component (e.g. the GUI's
// window geometry). A missing or malformed file yields an empty map.
func readViewerState() map[string]any {
	path, err := ViewerStatePath()
	if err != nil {
		return map[string]any{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	m := map[string]any{}
	if json.Unmarshal(StripJSONC(raw), &m) != nil {
		return map[string]any{}
	}
	return m
}

// LoadViewerExtendedSyntax returns the persisted extended-syntax toggle and
// whether it was present in state.jsonc. When ok is false the caller should
// fall back to the settings.jsonc default. A present-but-non-boolean value is
// treated as absent.
func LoadViewerExtendedSyntax() (value bool, ok bool) {
	m := readViewerState()
	v, present := m[stateKeyExtendedSyntax]
	if !present {
		return false, false
	}
	b, isBool := v.(bool)
	if !isBool {
		return false, false
	}
	return b, true
}

// SaveViewerExtendedSyntax persists the extended-syntax toggle into state.jsonc,
// preserving every other key already in the file (including GUI-owned window
// layout). Writes are best-effort; errors are ignored, mirroring the GUI's
// layout persistence.
func SaveViewerExtendedSyntax(value bool) {
	path, err := ViewerStatePath()
	if err != nil {
		return
	}
	m := readViewerState()
	m[stateKeyExtendedSyntax] = value
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append([]byte(viewerStateHeader), raw...), 0o644)
}
