//go:build windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerProcessRunsPowerShell(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "installer-ran.txt")
	script := "Set-Content -LiteralPath '" + strings.ReplaceAll(marker, "'", "''") + "' -Value 'started'"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	applyInstallerAttrs(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("PowerShell did not execute the installer command: %v", err)
	}
}
