//go:build windows

package console

import (
	"os"
	"syscall"
)

// hasConsole records whether mdv has somewhere to read/write text at startup -
// either a console (inherited from the launching terminal or reattached below)
// or std handles the shell redirected to a pipe/file. A GUI-subsystem binary
// double-clicked in Explorer has neither, which is how mode detection knows to
// default straight to the GUI without ever flashing a console window.
var hasConsole bool

// attachedConsole is true when the process was started from an interactive
// terminal and AttachConsole successfully reattached to the parent console
// without stdout already being redirected to a pipe or file. It is the
// reliable signal that the session is interactive on Windows, because the
// CONOUT$ handle opened afterwards may not pass term.IsTerminal.
var attachedConsole bool

// attachParentProcess is the ATTACH_PARENT_PROCESS sentinel ((DWORD)-1) passed
// to AttachConsole to reuse the console of the process that launched mdv.
const attachParentProcess = ^uintptr(0)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

// init wires up the standard streams for a GUI-subsystem (-H windowsgui) build.
//
// Building the launcher as a GUI subsystem binary is what prevents Windows from
// creating a console window when a .md file is double-clicked in Explorer. The
// cost is that such a binary is not connected to the terminal when run from a
// shell, so here we reattach to the parent console (and rebind os.Stdout/Stderr/
// Stdin to it) so --console, --tui and piping keep working from a terminal.
func init() {
	// Preserve any std handles the shell already redirected to a pipe or file;
	// those must win over the console so `mdv file.md > out.txt` still works.
	outRedirected := stdHandleValid(syscall.STD_OUTPUT_HANDLE)
	errRedirected := stdHandleValid(syscall.STD_ERROR_HANDLE)
	inRedirected := stdHandleValid(syscall.STD_INPUT_HANDLE)

	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		// No parent console (e.g. launched from Explorer). A console is still
		// "present" if output was redirected, so console dumps to that target
		// keep working; otherwise there is nowhere to print and GUI is implied.
		hasConsole = outRedirected
		return
	}
	hasConsole = true
	// Mark as attached-to-terminal only when stdout was not already redirected.
	// If stdout was redirected (e.g. mdv.exe > file.txt) we have a console but
	// the session is not interactive, so GUI should not open automatically.
	attachedConsole = !outRedirected

	if !outRedirected {
		// Open CONOUT$ read-write (not write-only): term.IsTerminal and
		// term.GetSize query the console via GetConsoleMode /
		// GetConsoleScreenBufferInfo, which require read access. A write-only
		// handle fails both, which would make console rendering fall back to the
		// no-color "notty" style at a fixed 80-column width.
		if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			os.Stdout = f
		}
	}
	if !errRedirected {
		if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			os.Stderr = f
		}
	}
	if !inRedirected {
		if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
			os.Stdin = f
		}
	}
}

// stdHandleValid reports whether the given standard handle is already bound to a
// real target (a pipe or file from shell redirection). A GUI-subsystem process
// with no redirection has null/invalid std handles.
func stdHandleValid(kind int) bool {
	h, err := syscall.GetStdHandle(kind)
	return err == nil && h != 0 && h != syscall.InvalidHandle
}

// HasStartupConsole reports whether mdv has a console or redirected output to
// write to. It is false only when launched from a GUI shell (Explorer) with no
// console and no redirection - the one case where GUI mode is the right default.
func HasStartupConsole() bool { return hasConsole }

// IsAttachedToTerminal reports true when the process was launched from an
// interactive terminal and successfully reattached to its parent console via
// AttachConsole without stdout being redirected. On GUI-subsystem binaries
// this is the only reliable way to detect an interactive terminal launch
// because the CONOUT$ handle obtained afterwards may not pass term.IsTerminal.
func IsAttachedToTerminal() bool { return attachedConsole }
