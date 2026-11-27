package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlausibleGeometry(t *testing.T) {
	cases := []struct {
		name       string
		x, y, w, h int
		want       bool
	}{
		{"normal window", 120, 60, 1200, 760, true},
		{"secondary monitor left of primary", -1920, 100, 1100, 780, true},
		{"minimum window size", 0, 0, 480, 360, true},
		{"minimized (DPI-scaled parking position)", -16000, -16000, 157, 25, false},
		{"minimized (unscaled parking position)", -32000, -32000, 160, 28, false},
		{"collapsed to caption on screen", 642, 409, 157, 35, false},
		{"zero size", 0, 0, 0, 0, false},
	}
	for _, c := range cases {
		if got := plausibleGeometry(c.x, c.y, c.w, c.h); got != c.want {
			t.Errorf("%s: plausibleGeometry(%d,%d,%d,%d) = %v, want %v", c.name, c.x, c.y, c.w, c.h, got, c.want)
		}
	}
}

func TestUpdateGeometryIgnoresMinimizedWindow(t *testing.T) {
	store := NewLayoutStore(LayoutState{X: 120, Y: 60, Width: 1200, Height: 760, Valid: true})
	defer func() {
		store.mu.Lock()
		if store.timer != nil {
			store.timer.Stop()
		}
		store.mu.Unlock()
	}()

	store.UpdateGeometry(-16000, -16000, 157, 25, false)
	got := store.Get()
	if got.X != 120 || got.Y != 60 || got.Width != 1200 || got.Height != 760 {
		t.Fatalf("minimized geometry was stored: %+v", got)
	}
}

func TestLoadLayoutStateDropsMinimizedGeometry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "mdv", "state.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := layoutStateHeader + `{"x": -16000, "y": -16000, "width": 157, "height": 25, "maximized": false,
"sidebarWidth": 300, "valid": true, "recent": [{"path": "a.md", "kind": "file"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	st := LoadLayoutState()
	if !st.Valid {
		t.Fatal("state should stay valid")
	}
	if st.X != 0 || st.Y != 0 || st.Width != 0 || st.Height != 0 {
		t.Errorf("implausible geometry not dropped: %+v", st)
	}
	if st.SidebarWidth != 300 || len(st.Recent) != 1 {
		t.Errorf("other settings must be kept: %+v", st)
	}
}
