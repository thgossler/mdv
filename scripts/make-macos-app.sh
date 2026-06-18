#!/usr/bin/env bash
#
# make-macos-app.sh — build `mdv.app`, a Finder-friendly application bundle.
#
# Why this exists:
#   The shipped `mdv` is a single Unix executable. macOS Finder's "Open With"
#   (and "set as default app for this file type") only offers *application
#   bundles* (.app) — a bare executable is always greyed out. This script wraps
#   the launcher in a minimal .app so Markdown files can be associated with mdv.
#
# How it works:
#   The bundle is a tiny AppleScript applet. When Finder opens a document it
#   delivers the file path via the Apple "odoc" event (NOT as a CLI argument);
#   the applet's `on open` handler forwards each path to the bundled launcher as
#   `mdv --gui <file>`. --gui is required because the applet's stdout is not a
#   TTY, which would otherwise make the launcher pick console mode. A copy of the
#   universal `mdv` launcher is embedded at Contents/MacOS/mdv so the bundle is
#   self-contained and works from anywhere (e.g. /Applications).
#
# The bundle declares the Markdown document type so it can be set as the default
# handler for .md/.markdown files, and uses images/icon.png as its app icon.
#
# Code signing / hardened runtime is applied when MDV_MACOS_SIGN_IDENTITY is set
# (a no-op otherwise, matching scripts/build.sh).
#
# Usage: scripts/make-macos-app.sh [app-path] [mdv-binary]
#   app-path     output bundle path (default: build/mdv.app)
#   mdv-binary   launcher to embed   (default: build/mdv)
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

APP="${1:-build/mdv.app}"
MDV_BIN="${2:-build/mdv}"
ICON_SRC="$ROOT/images/icon.png"
BUNDLE_ID="com.thgossler.mdv"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "make-macos-app: macOS only (needs osacompile, sips, iconutil)" >&2
  exit 1
fi
if [ ! -f "$MDV_BIN" ]; then
  echo "make-macos-app: launcher not found: $MDV_BIN" >&2
  exit 1
fi

# Derive a plain SemVer (no leading "v") for the bundle version keys.
if [ -f "$ROOT/VERSION" ]; then
  VERSION="$(tr -d ' \t\r\n' < "$ROOT/VERSION")"
else
  VERSION="0.0.0"
fi

echo "==> Building $APP (version $VERSION)"
rm -rf "$APP"

# --- 1. Compile the AppleScript applet (creates the base .app structure) -----
SCRIPT_TMP="$(mktemp -t mdv-applet).applescript"
trap 'rm -f "$SCRIPT_TMP"' EXIT
cat > "$SCRIPT_TMP" <<'APPLESCRIPT'
-- mdv launcher applet: forwards opened Markdown files to the embedded launcher.
on run
	launchWith({})
end run

on open theItems
	launchWith(theItems)
end open

on launchWith(theItems)
	set mdvBin to (POSIX path of (path to me)) & "Contents/MacOS/mdv"
	set q to quoted form of mdvBin
	if (count of theItems) is 0 then
		do shell script q & " --gui >/dev/null 2>&1 &"
	else
		repeat with f in theItems
			do shell script q & " --gui " & quoted form of (POSIX path of f) & " >/dev/null 2>&1 &"
		end repeat
	end if
end launchWith
APPLESCRIPT

osacompile -o "$APP" "$SCRIPT_TMP"

# osacompile names the stub executable "applet" for run-only scripts and
# "droplet" when an `on open` handler is present. Capture whichever it chose so
# the rewritten Info.plist and signing below reference the real binary.
STUB="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$APP/Contents/Info.plist")"

# --- 2. Embed the launcher binary -------------------------------------------
cp "$MDV_BIN" "$APP/Contents/MacOS/mdv"
chmod 0755 "$APP/Contents/MacOS/mdv"

# --- 3. Build the app icon (icon.png -> AppIcon.icns) ------------------------
ICONSET="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$ICON_SRC" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  sips -z "$((size * 2))" "$((size * 2))" "$ICON_SRC" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"
rm -rf "$(dirname "$ICONSET")"
# Drop the default droplet icon assets so Finder uses AppIcon.icns unambiguously.
rm -f "$APP/Contents/Resources/droplet.icns" "$APP/Contents/Resources/Assets.car"

# --- 4. Write the Info.plist (declares the Markdown document type) -----------
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>mdv</string>
	<key>CFBundleDisplayName</key>
	<string>mdv</string>
	<key>CFBundleIdentifier</key>
	<string>${BUNDLE_ID}</string>
	<key>CFBundleVersion</key>
	<string>${VERSION}</string>
	<key>CFBundleShortVersionString</key>
	<string>${VERSION}</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleExecutable</key>
	<string>${STUB}</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<key>LSMinimumSystemVersion</key>
	<string>10.15</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<!-- Run as an agent: the wrapper only forwards the opened file to the GUI and
	     then exits, so it must not bounce its own icon in the Dock. Without this,
	     opening a file shows the wrapper's Dock icon briefly before the GUI's
	     icon appears. The GUI helper (a separate process) still shows its own
	     Dock icon and window normally. -->
	<key>LSUIElement</key>
	<true/>
	<key>CFBundleDocumentTypes</key>
	<array>
		<dict>
			<key>CFBundleTypeName</key>
			<string>Markdown document</string>
			<key>CFBundleTypeRole</key>
			<string>Viewer</string>
			<key>LSHandlerRank</key>
			<string>Alternate</string>
			<!-- Reference the system-registered Markdown UTI directly. Declaring
			     raw CFBundleTypeExtensions instead makes LaunchServices synthesize
			     a dynamic UTI (e.g. "dyn.ag…"), which is what then appears in
			     Finder's "Change All" dialog instead of "Markdown document". -->
			<key>LSItemContentTypes</key>
			<array>
				<string>net.daringfireball.markdown</string>
			</array>
		</dict>
	</array>
</dict>
</plist>
PLIST

# Drop the AppleScript editability hint osacompile leaves behind so the applet
# launches the script directly without offering to edit it.
/usr/libexec/PlistBuddy -c "Delete :WindowState" "$APP/Contents/Info.plist" 2>/dev/null || true

# --- 5. Code sign (hardened runtime), when an identity is configured ---------
MDV_MACOS_ENTITLEMENTS="${MDV_MACOS_ENTITLEMENTS:-$ROOT/scripts/macos-entitlements.plist}"
if [ -n "${MDV_MACOS_SIGN_IDENTITY:-}" ]; then
  echo "    codesign: bundle contents + $APP"
  # Sign inner Mach-O files first, then seal the bundle.
  codesign --force --options runtime --timestamp \
    --entitlements "$MDV_MACOS_ENTITLEMENTS" \
    --sign "$MDV_MACOS_SIGN_IDENTITY" "$APP/Contents/MacOS/mdv"
  codesign --force --options runtime --timestamp \
    --entitlements "$MDV_MACOS_ENTITLEMENTS" \
    --sign "$MDV_MACOS_SIGN_IDENTITY" "$APP/Contents/MacOS/$STUB"
  codesign --force --options runtime --timestamp \
    --entitlements "$MDV_MACOS_ENTITLEMENTS" \
    --sign "$MDV_MACOS_SIGN_IDENTITY" "$APP"
  codesign --verify --strict --verbose=2 "$APP" || true
fi

echo "==> Done: $APP"
