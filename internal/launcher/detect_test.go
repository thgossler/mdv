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
