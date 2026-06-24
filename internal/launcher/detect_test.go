package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModeString(t *testing.T) {
	cases := map[Mode]string{
		ModeConsole: "console",
		ModeTUI:     "tui",
		ModeGUI:     "gui",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}

func TestDetectModeExplicitFlags(t *testing.T) {
	if got := DetectMode(Preference{ForceConsole: true}); got != ModeConsole {
		t.Errorf("ForceConsole -> %v, want ModeConsole", got)
	}
	if got := DetectMode(Preference{ForceTUI: true}); got != ModeTUI {
		t.Errorf("ForceTUI -> %v, want ModeTUI", got)
	}
}

func TestDetectModeNonInteractiveDefaultsToConsole(t *testing.T) {
	// Under `go test` stdout is a pipe, not a TTY, so with no explicit
	// preference the launcher must choose console output. This is the exact
	// guarantee that keeps mdv usable in headless CI/containers.
	if got := DetectMode(Preference{}); got != ModeConsole {
		t.Errorf("non-interactive default -> %v, want ModeConsole", got)
	}
}

func TestDetectModeForceGUINonInteractive(t *testing.T) {
	// Under `go test` stdout is not a TTY. A forced GUI must therefore resolve
	// to either ModeGUI (when a GUI environment is available) or, when it is
	// not, degrade straight to ModeConsole - never ModeTUI, since the TUI
	// fallback requires an interactive stdout.
	got := DetectMode(Preference{ForceGUI: true})
	if got != ModeGUI && got != ModeConsole {
		t.Errorf("ForceGUI non-interactive -> %v, want ModeGUI or ModeConsole", got)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(existing) {
		t.Errorf("fileExists(%q) = false, want true", existing)
	}
	if fileExists(filepath.Join(dir, "absent.txt")) {
		t.Error("fileExists(missing) = true, want false")
	}
}

func TestParseGTK4Minor(t *testing.T) {
	cases := []struct {
		name      string
		wantMinor int
		wantOK    bool
	}{
		{"libgtk-4.so.1.1400.2", 14, true}, // GTK 4.14.2
		{"libgtk-4.so.1.1000.0", 10, true}, // GTK 4.10.0
		{"libgtk-4.so.1.800.3", 8, true},   // GTK 4.8.3
		{"libgtk-4.so.1", 0, false},        // bare soname, no version
		{"libgtk-4.so.1.1400", 0, false},   // no micro component
		{"libgtk-3.so.0.2400.0", 0, false}, // GTK 3, not matched
		{"libwebkitgtk-6.0.so.4", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		gotMinor, gotOK := parseGTK4Minor(c.name)
		if gotMinor != c.wantMinor || gotOK != c.wantOK {
			t.Errorf("parseGTK4Minor(%q) = (%d, %v), want (%d, %v)",
				c.name, gotMinor, gotOK, c.wantMinor, c.wantOK)
		}
	}
}

func TestGTK4MinorInDirs(t *testing.T) {
	// No GTK library present: unavailable.
	empty := t.TempDir()
	if minor, ok := gtk4MinorInDirs([]string{empty}); ok {
		t.Errorf("gtk4MinorInDirs(empty) = (%d, true), want (_, false)", minor)
	}

	// Versioned object present: its minor is returned, even when an older
	// duplicate exists in another directory (highest wins).
	newDir := t.TempDir()
	oldDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "libgtk-4.so.1.1400.2"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "libgtk-4.so.1.800.3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	minor, ok := gtk4MinorInDirs([]string{oldDir, newDir})
	if !ok || minor != 14 {
		t.Errorf("gtk4MinorInDirs(new+old) = (%d, %v), want (14, true)", minor, ok)
	}

	// Only the bare soname symlink is exposed: follow it to the version.
	linkDir := t.TempDir()
	target := filepath.Join(linkDir, "libgtk-4.so.1.1000.0")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("libgtk-4.so.1.1000.0", filepath.Join(linkDir, "libgtk-4.so.1")); err != nil {
		t.Fatal(err)
	}
	if minor, ok := gtk4MinorInDirs([]string{linkDir}); !ok || minor != 10 {
		t.Errorf("gtk4MinorInDirs(symlink) = (%d, %v), want (10, true)", minor, ok)
	}
}

func TestGTK4AtLeast(t *testing.T) {
	// gtk4AtLeast must never panic and returns a bool reflecting this host. The
	// detailed version logic is covered by gtk4MinorInDirs above; here we only
	// guard the public predicate against the live system.
	_ = gtk4AtLeast(minGTK4Minor)
}
