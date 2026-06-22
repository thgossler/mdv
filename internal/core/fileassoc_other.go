//go:build !windows && !darwin

package core

// registerFileAssociations is a no-op on platforms without a supported file
// manager integration. Linux desktops vary widely (and associations are managed
// via .desktop files installed by packagers), so mdv does not modify them here.
func registerFileAssociations(exe string) error { return nil }
