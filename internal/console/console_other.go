//go:build !windows

package console

// HasStartupConsole reports whether mdv has a console or redirected output to
// write to. On non-Windows platforms a process always has a controlling stream
// (or a pipe), so this is always true and mode detection falls back to its
// normal TTY-based logic.
func HasStartupConsole() bool { return true }
