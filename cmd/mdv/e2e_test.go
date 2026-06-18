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

// runMDVStdin executes the built binary with stdin fed from the given string,
// returning stdout+stderr and the exit code.
func runMDVStdin(t *testing.T, stdin string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(mdvBinary(t), args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
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

func TestE2EStdinConsoleRender(t *testing.T) {
	md := "# Piped Heading\n\nRendered from **stdin**.\n"
	out, code := runMDVStdin(t, md, []string{"NO_COLOR=1"}, "--console")
	if code != 0 {
		t.Fatalf("stdin --console exit code = %d, want 0 (output: %s)", code, out)
	}
	if !strings.Contains(out, "Piped Heading") {
		t.Errorf("stdin console output missing heading: %q", out)
	}
	if !strings.Contains(out, "Rendered from") {
		t.Errorf("stdin console output missing body: %q", out)
	}
}

func TestE2EStdinEmptyShowsUsage(t *testing.T) {
	out, code := runMDVStdin(t, "   \n", nil, "--console")
	if code != 2 {
		t.Fatalf("empty stdin exit code = %d, want 2 (output: %s)", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "usage") {
		t.Errorf("empty stdin output should contain usage: %q", out)
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

func TestReorderArgsMaxWidthValue(t *testing.T) {
	// --max-width takes a value; reorderArgs must keep the value adjacent to
	// the flag even when the input path sits between them.
	got := reorderArgs([]string{"file.md", "--max-width", "80", "--console"})
	want := []string{"--max-width", "80", "--console", "file.md"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("reorderArgs = %v, want %v", got, want)
	}
	// --max-width=80 form stays a single token.
	got = reorderArgs([]string{"file.md", "--max-width=80"})
	want = []string{"--max-width=80", "file.md"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("reorderArgs(=form) = %v, want %v", got, want)
	}
}

func TestReorderArgsExtra(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"trailing flags", []string{"file.md", "--console", "--tui"}, []string{"--console", "--tui", "file.md"}},
		{"already ordered", []string{"--console", "file.md"}, []string{"--console", "file.md"}},
		{"terminator stops parsing", []string{"--", "--gui", "file.md"}, []string{"--gui", "file.md"}},
		{"multiple value flags", []string{"file.md", "--max-width", "100", "--images", "auto"}, []string{"--max-width", "100", "--images", "auto", "file.md"}},
		{"value flag mid-positionals", []string{"--console", "file.md", "--max-width", "80"}, []string{"--console", "--max-width", "80", "file.md"}},
		{"empty", []string{}, []string{}},
		{"only positional", []string{"file.md"}, []string{"file.md"}},
	}
	for _, c := range cases {
		got := reorderArgs(c.in)
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: reorderArgs(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestE2EMaxWidthCapsOutput(t *testing.T) {
	body := "# Heading\n\nThis is a reasonably long paragraph that would otherwise wrap to the full terminal width when rendered by mdv in console mode.\n"
	// Pipe via stdin so there is no file-path header line to skew the check.
	out, code := runMDVStdin(t, body, []string{"NO_COLOR=1"}, "--console", "--max-width", "40")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (output: %s)", code, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := len([]rune(strings.TrimRight(l, " "))); w > 40 {
			t.Errorf("line exceeds max-width 40 (%d cols): %q", w, l)
		}
	}
}
