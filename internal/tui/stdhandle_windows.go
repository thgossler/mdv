//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// bindStdHandle repoints the process's Win32 standard handle for the given role
// at the supplied file. Bubble Tea's Windows console input reader resolves the
// console through windows.GetStdHandle(STD_INPUT_HANDLE) (via coninput) rather
// than os.Stdin, so when stdin is a pipe, reassigning os.Stdin alone is not
// enough: GetConsoleMode is still called on the redirected pipe handle and fails
// with "The handle is invalid". Repointing the standard handle at the reopened
// console device (CONIN$/CONOUT$) makes Bubble Tea see the real console.
func bindStdHandle(role ttyRole, f *os.File) {
	which := uint32(windows.STD_INPUT_HANDLE)
	if role == ttyOutput {
		which = windows.STD_OUTPUT_HANDLE
	}
	_ = windows.SetStdHandle(which, windows.Handle(f.Fd()))
}
