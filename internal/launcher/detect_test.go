package launcher

import "testing"

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
