package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadViewerExtendedSyntaxMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if _, ok := LoadViewerExtendedSyntax(); ok {
		t.Errorf("expected ok=false when state.jsonc is absent")
	}
}

func TestSaveAndLoadViewerExtendedSyntax(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	SaveViewerExtendedSyntax(true)
	v, ok := LoadViewerExtendedSyntax()
	if !ok || !v {
		t.Fatalf("expected (true,true), got (%v,%v)", v, ok)
	}

	SaveViewerExtendedSyntax(false)
	v, ok = LoadViewerExtendedSyntax()
	if !ok || v {
		t.Fatalf("expected (false,true), got (%v,%v)", v, ok)
	}
}

// SaveViewerExtendedSyntax must not drop keys owned by another component (the
// GUI's window layout lives in the same state.jsonc file).
func TestSaveViewerExtendedSyntaxPreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, AppName)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "// header\n" + `{
		"width": 1200,
		"height": 800,
		"sidebarWidth": 300,
		"valid": true,
	}`
	statePath := filepath.Join(cfgDir, "state.jsonc")
	if err := os.WriteFile(statePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	SaveViewerExtendedSyntax(true)

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(StripJSONC(raw), &m); err != nil {
		t.Fatalf("rewritten state is not valid JSONC: %v", err)
	}
	if m["width"] != float64(1200) || m["height"] != float64(800) || m["sidebarWidth"] != float64(300) {
		t.Errorf("layout keys were dropped: %+v", m)
	}
	if m["valid"] != true {
		t.Errorf("valid flag was dropped: %+v", m)
	}
	if m[stateKeyExtendedSyntax] != true {
		t.Errorf("extendedSyntax not persisted: %+v", m)
	}
}
