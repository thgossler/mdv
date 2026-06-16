package core

import (
	"fmt"
	"os/exec"
	"runtime"
)

// hostOSArch returns lowercase OS and arch tokens used to match release assets.
func hostOSArch() (string, string) {
	goos := runtime.GOOS
	switch goos {
	case "darwin":
		goos = "darwin"
	}
	goarch := runtime.GOARCH
	switch goarch {
	case "amd64":
		// Match both "amd64" and "x64"/"x86_64" naming via the caller's Contains.
	}
	return goos, goarch
}

// OpenInOS opens a file path or URL using the operating system's default
// handler (browser for URLs, default app for files). It is used in every mode
// to follow non-markdown links.
func OpenInOS(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default: // linux, bsd
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening %q: %w", target, err)
	}
	// Reap the process so it does not become a zombie; ignore its exit.
	go func() { _ = cmd.Wait() }()
	return nil
}

// SpawnDetached launches exe with the given arguments as an independent
// process, not tied to the lifetime of the caller. It is used to open a
// document in a new window (a separate mdv instance).
func SpawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawning %q: %w", exe, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
