package main

// End-to-end tests that build the real `mdv` launcher and exercise it as a
// subprocess, the way a user (or a CI quality gate) would. These verify the
// headless-safe contract: the binary always starts and renders to stdout when
// no GUI/TTY is available.
//
// The build is done once and shared across tests. Run `go test -short` to skip
// these (they compile the binary, which is slower than the unit tests).

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// mdvBinary compiles cmd/mdv once and returns the path to the executable.
func mdvBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping end-to-end build test in -short mode")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mdv-e2e")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(dir, "mdv")
		if os.PathSeparator == '\\' {
			out += ".exe" // give the binary a .exe suffix on Windows so it runs
		}
		cmd := exec.Command("go", "build", "-o", out, ".")
		if combined, err := cmd.CombinedOutput(); err != nil {
			buildErr = err
			binPath = string(combined)
			return
		}
		binPath = out
	})
	if buildErr != nil {
		t.Fatalf("building mdv failed: %v\n%s", buildErr, binPath)
	}
	return binPath
}

// runMDV executes the built binary and returns stdout+stderr and the exit code.
func runMDV(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(mdvBinary(t), args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running mdv %v: %v", args, err)
		}
	}
	return string(out), code
}

func TestE2EVersion(t *testing.T) {
	out, code := runMDV(t, nil, "--version")
	if code != 0 {
		t.Fatalf("--version exit code = %d, want 0 (output: %s)", code, out)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(strings.TrimSpace(out)) {
		t.Errorf("--version output should be a bare SemVer: %q", out)
	}
}

func TestE2ENoArgShowsUsageWithExit2(t *testing.T) {
	out, code := runMDV(t, nil)
	if code != 2 {
		t.Fatalf("no-arg exit code = %d, want 2 (output: %s)", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "usage") {
		t.Errorf("no-arg output should contain usage: %q", out)
	}
}

func TestE2EConsoleRender(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	body := "# E2E Heading\n\nHello from **mdv** end-to-end.\n"
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Force console mode and disable color for a stable, headless render.
	out, code := runMDV(t, []string{"NO_COLOR=1"}, "--console", doc)
	if code != 0 {
		t.Fatalf("--console exit code = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "E2E Heading") {
		t.Errorf("console output missing heading: %q", out)
	}
	if !strings.Contains(out, "Hello from") {
		t.Errorf("console output missing body text: %q", out)
	}
}

func TestE2EFlagAfterInputArg(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	body := "# Trailing Flag\n\nForced console mode via a trailing flag.\n"
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// The flag follows the positional argument; it must still take effect.
	out, code := runMDV(t, []string{"NO_COLOR=1"}, doc, "--console")
	if code != 0 {
		t.Fatalf("trailing --console exit code = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "Trailing Flag") {
		t.Errorf("console output missing heading: %q", out)
	}
}

func TestE2EInitConfig(t *testing.T) {
	cfgRoot := t.TempDir()
	out, code := runMDV(t, []string{"XDG_CONFIG_HOME=" + cfgRoot}, "--init-config")
	if code != 0 {
		t.Fatalf("--init-config exit code = %d, want 0 (output: %s)", code, out)
	}
	written := filepath.Join(cfgRoot, "mdv", "settings.jsonc")
	if _, err := os.Stat(written); err != nil {
		t.Errorf("--init-config did not write %s: %v", written, err)
	}
}
