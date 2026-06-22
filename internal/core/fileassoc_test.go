package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileAssocVersionRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if v := fileAssocVersionFromState(); v != 0 {
		t.Fatalf("version with no state file = %d, want 0", v)
	}
	if err := setFileAssocVersion(FileAssocSchemeVersion); err != nil {
		t.Fatalf("setFileAssocVersion: %v", err)
	}
	if v := fileAssocVersionFromState(); v != FileAssocSchemeVersion {
		t.Fatalf("version after set = %d, want %d", v, FileAssocSchemeVersion)
	}
}

// setFileAssocVersion must not discard layout keys written by the GUI; the two
// writers share state.jsonc.
func TestSetFileAssocVersionPreservesExistingKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, err := statePath()
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const existing = `// header
{
  "width": 1234,
  "sidebarWidth": 321
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if err := setFileAssocVersion(7); err != nil {
		t.Fatalf("setFileAssocVersion: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(StripJSONC(raw), &m); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if m["width"] != float64(1234) || m["sidebarWidth"] != float64(321) {
		t.Errorf("existing keys not preserved: %v", m)
	}
	if m[fileAssocStateKey] != float64(7) {
		t.Errorf("%s = %v, want 7", fileAssocStateKey, m[fileAssocStateKey])
	}
}
