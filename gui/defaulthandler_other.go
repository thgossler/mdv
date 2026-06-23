//go:build !windows && !darwin

package main

// isDefaultHandler reports whether mdv is the OS default handler for path's
// type. mdv does not manage file associations on these platforms (Linux desktop
// associations are owned by packagers via .desktop files), so it conservatively
// returns false, which makes the GUI always offer the external-open button.
func isDefaultHandler(string) bool { return false }
