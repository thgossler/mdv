//go:build gui_bundled && darwin

package launcher

import (
	_ "embed"
	"os"
	"path/filepath"
)

// macAppIcon is the application icon (built from images/icon.png) used for the
// GUI helper's .app bundle so macOS shows it in the Dock and Cmd+Tab switcher.
//
//go:embed macicon/mdv.icns
var macAppIcon []byte

// macBundleInfoPlist is the minimal Info.plist that turns the extracted helper
// into a proper macOS application. CFBundleIconFile + the bundled .icns give it
// a real icon, and CFBundleName makes it present itself as "mdv" everywhere.
const macBundleInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>mdv</string>
	<key>CFBundleDisplayName</key>
	<string>mdv</string>
	<key>CFBundleIdentifier</key>
	<string>de.thomas-gossler.apps.mdv</string>
	<key>CFBundleExecutable</key>
	<string>mdv</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleIconFile</key>
	<string>mdv</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>LSMinimumSystemVersion</key>
	<string>10.15</string>
</dict>
</plist>
`

// finalizeHelper wraps the extracted helper in a minimal .app bundle alongside
// it (e.g. <cache>/mdv/gui/<version>/mdv.app) and returns the path to the
// executable inside the bundle. macOS only shows a custom application icon and
// name for executables that live inside an .app bundle with an Info.plist; a
// bare binary always displays the generic command-line-tool icon in the Dock
// and Cmd+Tab application switcher.
func finalizeHelper(exe string) (string, error) {
	dir := filepath.Dir(exe)
	app := filepath.Join(dir, "mdv.app")
	macOS := filepath.Join(app, "Contents", "MacOS")
	resources := filepath.Join(app, "Contents", "Resources")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		return "", err
	}
	if err := writeIfChanged(filepath.Join(app, "Contents", "Info.plist"), []byte(macBundleInfoPlist), 0o644); err != nil {
		return "", err
	}
	if err := writeIfChanged(filepath.Join(resources, "mdv.icns"), macAppIcon, 0o644); err != nil {
		return "", err
	}

	inner := filepath.Join(macOS, "mdv")
	if err := linkOrCopy(exe, inner); err != nil {
		return "", err
	}
	_ = os.Chmod(inner, 0o755)
	return inner, nil
}

// writeIfChanged writes data to path only when the file is missing or differs,
// using an atomic rename so a concurrent launch never observes a partial file.
func writeIfChanged(path string, data []byte, perm os.FileMode) error {
	if fileMatches(path, data) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// linkOrCopy makes dst hold the same bytes as src, preferring a hard link (no
// extra disk use) and falling back to a copy when linking is not possible. It
// is a no-op when dst already matches src.
func linkOrCopy(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if fileMatches(dst, data) {
		return nil
	}
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
