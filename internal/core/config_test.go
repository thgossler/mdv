package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	in := []byte(`{
		// a line comment
		"theme": "dark", /* block */ "fontSizePx": 18,
		"note": "http://not-a-comment", // trailing comment
		"list": [1, 2, 3,], // trailing comma in array
	}`)
	out := stripJSONC(in)

	// The cleaned output must be valid JSON and preserve string contents that
	// look like comments.
	got := string(out)
	if want := `"http://not-a-comment"`; !strings.Contains(got, want) {
		t.Errorf("stripJSONC removed content inside a string: %s", got)
	}
	if strings.Contains(got, "// a line comment") || strings.Contains(got, "/* block */") {
		t.Errorf("stripJSONC left comments in output: %s", got)
	}
	if strings.Contains(got, ",}") || strings.Contains(got, ",]") {
		t.Errorf("stripJSONC left trailing commas: %s", got)
	}
}

func TestLoadConfigDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig with no file: %v", err)
	}
	def := DefaultSettings()
	if cfg.Theme != def.Theme || cfg.FontSizePx != def.FontSizePx {
		t.Errorf("missing config should yield defaults, got %+v", cfg)
	}
}

func TestLoadConfigMergesOverDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, AppName)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
		// only override two keys
		"theme": "dark",
		"fontSizePx": 20,
	}`
	if err := os.WriteFile(filepath.Join(cfgDir, "settings.jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Theme != "dark" {
		t.Errorf("theme override = %q, want dark", cfg.Theme)
	}
	if cfg.FontSizePx != 20 {
		t.Errorf("fontSizePx override = %v, want 20", cfg.FontSizePx)
	}
	// Untouched keys keep their defaults.
	if cfg.LineHeight != DefaultSettings().LineHeight {
		t.Errorf("lineHeight = %v, want default %v", cfg.LineHeight, DefaultSettings().LineHeight)
	}
}

func TestLoadConfigMalformedReturnsDefaultsAndError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, AppName)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "settings.jsonc"), []byte(`{ "theme": `), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err == nil {
		t.Fatal("expected an error for malformed config")
	}
	if cfg.Theme != DefaultSettings().Theme {
		t.Errorf("malformed config should fall back to defaults, got %+v", cfg)
	}
}

func TestWriteDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := WriteDefaultConfig()
	if err != nil {
		t.Fatalf("WriteDefaultConfig: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}

	// The written template must round-trip through the loader.
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after write: %v", err)
	}
	if cfg.Theme == "" {
		t.Error("loaded config from written template has empty theme")
	}

	// Writing again must not clobber an existing file.
	before, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(before, []byte("\n// touched\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDefaultConfig(); err != nil {
		t.Fatalf("second WriteDefaultConfig: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "// touched") {
		t.Error("WriteDefaultConfig overwrote an existing file")
	}
}
