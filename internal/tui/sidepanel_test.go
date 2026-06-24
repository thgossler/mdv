package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thgossler/mdv/internal/core"
)

// TestForceSidePanelShowsNavigatorForFile verifies that --sidepanel
// (cfg.ForceSidePanel) makes the document navigator start visible even when the
// TUI is opened on a single file, where it would otherwise be hidden.
func TestForceSidePanelShowsNavigatorForFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	in := core.Input{Kind: core.InputFile, Path: path, Dir: dir}

	cfg := core.DefaultSettings()
	if m := New(cfg, in, core.UpdateInfo{}); m.showList {
		if m.watcher != nil {
			m.watcher.Close()
		}
		t.Fatal("navigator should start hidden for a single file without --sidepanel")
	}

	cfg.ForceSidePanel = true
	m := New(cfg, in, core.UpdateInfo{})
	if m.watcher != nil {
		defer m.watcher.Close()
	}
	if !m.showList {
		t.Fatal("--sidepanel should force the navigator visible for a single file")
	}
}
