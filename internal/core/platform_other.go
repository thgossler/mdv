//go:build !windows

package core

import (
	"os/exec"
	"syscall"
)

// applyDetachedAttrs starts the child in a new session so it has no controlling
// terminal. Combined with the null-device standard streams set in
// SpawnDetached, this detaches the spawned GUI helper from the launcher's
// terminal - mirroring the Windows DETACHED_PROCESS behaviour - so it cannot
// hold the tty or interfere with the shell's line editing after mdv exits.
func applyDetachedAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
