//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// Process creation flags (from <winbase.h>). DETACHED_PROCESS gives the child
// no console at all - it neither inherits the launcher's console nor creates a
// new one - so spawning the GUI helper can never leave a console window behind
// (or tie up the terminal when mdv is run from a shell).
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// applyDetachedAttrs configures the spawned process to be fully independent of
// the launcher's console.
func applyDetachedAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}
