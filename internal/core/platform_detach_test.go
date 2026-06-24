//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// spawnChildEnv, when set, makes the test binary act as a helper child that
// records its session id and standard-input device, then exits. It backs
// TestSpawnDetachedDetachesFromTerminal.
const spawnChildEnv = "MDV_TEST_SPAWN_CHILD_OUT"

// TestMain lets the test binary double as the spawned child process: when
// spawnChildEnv is set it writes diagnostic facts about the child's terminal
// detachment and exits before any tests run.
func TestMain(m *testing.M) {
	if out := os.Getenv(spawnChildEnv); out != "" {
		runSpawnChild(out)
		return
	}
	os.Exit(m.Run())
}

// runSpawnChild records whether the child was placed in its own session and
// whether its stdin still refers to a terminal, then exits.
func runSpawnChild(out string) {
	sid, _ := unix.Getsid(0)
	isTTY := "0"
	if term.IsTerminal(0) {
		isTTY = "1"
	}
	_ = os.WriteFile(out, []byte(strconv.Itoa(sid)+"\n"+isTTY+"\n"), 0o600)
	os.Exit(0)
}

// TestSpawnDetachedDetachesFromTerminal verifies that a process launched via
// SpawnDetached runs in its own session (so it has no controlling terminal) and
// does not inherit an interactive stdin - the guarantee that stops a spawned GUI
// helper from holding the tty and disturbing the shell after mdv exits.
func TestSpawnDetachedDetachesFromTerminal(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	out := filepath.Join(t.TempDir(), "child.txt")
	t.Setenv(spawnChildEnv, out)

	if err := SpawnDetached(self); err != nil {
		t.Fatalf("SpawnDetached: %v", err)
	}

	data := waitForFile(t, out)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected child output %q", string(data))
	}

	childSID, err := strconv.Atoi(lines[0])
	if err != nil {
		t.Fatalf("parsing child session id %q: %v", lines[0], err)
	}
	parentSID, _ := unix.Getsid(0)
	if childSID == parentSID {
		t.Errorf("child session id %d equals parent %d; child was not detached", childSID, parentSID)
	}

	if lines[1] != "0" {
		t.Errorf("child stdin is a terminal; it should be the null device")
	}
}

// waitForFile polls until path exists and is non-empty, failing the test on
// timeout. The child runs asynchronously, so a short wait avoids flakiness.
func waitForFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child did not write %s in time", path)
	return nil
}
