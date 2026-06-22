//go:build darwin

package core

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// lsregisterPath is the Launch Services helper that (re)registers an app bundle
// so Finder offers it under "Open With" for its declared document types.
const lsregisterPath = "/System/Library/Frameworks/CoreServices.framework/" +
	"Frameworks/LaunchServices.framework/Support/lsregister"

// registerFileAssociations makes mdv appear in Finder's "Open With" submenu for
// Markdown files. macOS has no per-item context-menu verb API the way Windows
// does; the supported equivalent is the "Open With" entry, populated by Launch
// Services from the app bundle's declared document types (see the Info.plist
// built by scripts/make-macos-app.sh, which covers .md, .mdx and .markdown).
// Here we force-register that bundle so the entry appears without waiting for a
// background re-scan. When mdv runs as a bare binary (e.g. installed to
// /usr/local/bin) there is no bundle to register, so this is a no-op.
func registerFileAssociations(exe string) error {
	bundle := appBundlePath(exe)
	if bundle == "" {
		return nil
	}
	return exec.Command(lsregisterPath, "-f", bundle).Run()
}

// appBundlePath returns the enclosing .app bundle for exe (whose layout is
// <bundle>/Contents/MacOS/<exe>), or "" when exe is not inside one.
func appBundlePath(exe string) string {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	macOS := filepath.Dir(exe) // <bundle>/Contents/MacOS
	if filepath.Base(macOS) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(macOS) // <bundle>/Contents
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	bundle := filepath.Dir(contents) // <bundle>.app
	if !strings.HasSuffix(bundle, ".app") {
		return ""
	}
	return bundle
}
