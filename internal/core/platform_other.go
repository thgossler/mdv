//go:build !windows

package core

import "os/exec"

// applyDetachedAttrs is a no-op on non-Windows platforms, where spawning a child
// does not create or attach a console window.
func applyDetachedAttrs(cmd *exec.Cmd) {}
