//go:build !windows

package tui

import "os"

// bindStdHandle is a no-op on platforms where the controlling terminal is a
// single device (/dev/tty) and reassigning os.Stdin/os.Stdout is sufficient.
func bindStdHandle(_ ttyRole, _ *os.File) {}
