#!/usr/bin/env bash
#
# build.sh - produce a self-contained `mdv` executable for the host platform.
#
# Pipeline:
#   1. Build the Wails GUI frontend (TypeScript -> static assets).
#   2. Compile the GUI helper binary (embeds the frontend via go:embed).
#   3. Gzip the helper into internal/launcher/assets/mdv-gui.gz.
#   4. Compile the launcher with -tags gui_bundled, embedding the helper.
#
# The result `build/mdv` starts headless-safe (pure-Go launcher with no webview
# linkage) and spawns the embedded GUI helper only when a GUI environment is
# detected.
#
# Usage: scripts/build.sh [version]
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
# Version precedence: explicit arg > VERSION file (canonical SemVer) > fallback.
if [ "${1:-}" != "" ]; then
  VERSION="$1"
elif [ -f "$ROOT/VERSION" ]; then
  VERSION="v$(tr -d ' \t\r\n' < "$ROOT/VERSION")"
else
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo v0.0.0-dev)"
fi
LDFLAGS="-s -w -X github.com/thgossler/mdv/internal/core.Version=${VERSION}"

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
HELPER_EXT=""
[ "$GOOS" = "windows" ] && HELPER_EXT=".exe"
# On Windows, mark both executables as GUI-subsystem binaries so no console
# window is created when a .md file is double-clicked in Explorer. The launcher
# reattaches to the parent console at startup for terminal use (see
# internal/console/console_windows.go).
WINGUI_LDFLAGS=""
[ "$GOOS" = "windows" ] && WINGUI_LDFLAGS=" -H windowsgui"

# macos_sign code-signs a Mach-O file with the Developer ID identity in
# MDV_MACOS_SIGN_IDENTITY (a no-op when that variable is unset, e.g. local dev
# builds). Both the embedded GUI helper and the final launcher are signed so the
# hardened runtime and Gatekeeper accept them; the helper must be signed *before*
# it is gzipped into the launcher.
MDV_MACOS_ENTITLEMENTS="${MDV_MACOS_ENTITLEMENTS:-$ROOT/scripts/macos-entitlements.plist}"
macos_sign() {
  local file="$1"
  [ "$GOOS" = "darwin" ] || return 0
  [ -n "${MDV_MACOS_SIGN_IDENTITY:-}" ] || return 0
  echo "    codesign: $file"
  codesign --force --options runtime --timestamp \
    --entitlements "$MDV_MACOS_ENTITLEMENTS" \
    --sign "$MDV_MACOS_SIGN_IDENTITY" "$file"
}

mkdir -p build internal/launcher/assets

# Keep the icon the GUI embeds (gui/appicon.png, used for the macOS Dock icon and
# the Linux window icon) in sync with the canonical source.
cp images/icon.png gui/appicon.png

echo "==> [1/4] Building frontend"
pushd gui/frontend >/dev/null
if [ ! -d node_modules ]; then
  npm ci || npm install
fi
npm run build
popd >/dev/null

# Safety net for the go:embed placeholder. Vite copies gui/frontend/public/ into
# dist/ on every build, which normally regenerates dist/.gitkeep after Vite
# empties the output directory. This guard keeps the embed working even if that
# copy ever stops happening: CI runs `go vet` on the //go:embed all:frontend/dist
# directive WITHOUT building the frontend, so a missing dist/.gitkeep would fail.
if [ ! -f gui/frontend/dist/.gitkeep ]; then
  cp gui/frontend/public/.gitkeep gui/frontend/dist/.gitkeep
fi

# Stage the freshly built frontend as the embedded "print" harness used by the
# headless-browser PDF engine (compiled in via -tags pdf_bundled below). Both the
# GUI helper and the launcher import internal/pdf, so both embed this bundle.
echo "==> Staging print bundle for PDF export"
rm -rf internal/pdf/assets/dist
cp -R gui/frontend/dist internal/pdf/assets/dist

echo "==> [2/4] Generating bindings + compiling GUI helper"
( cd gui && wails3 generate bindings -ts -i -clean=true >/dev/null 2>&1 || true )

build_macos_universal() {
  # Build the GUI helper as a universal (arm64 + amd64) binary.
  local out_helper="$1"
  echo "    macOS universal: building arm64 + amd64 slices"
  CGO_ENABLED=1 GOARCH=arm64 go build -tags "production pdf_bundled" -ldflags "$LDFLAGS" -o "build/mdv-gui-arm64" ./gui
  CGO_ENABLED=1 GOARCH=amd64 go build -tags "production pdf_bundled" -ldflags "$LDFLAGS" -o "build/mdv-gui-amd64" ./gui
  lipo -create -output "$out_helper" build/mdv-gui-arm64 build/mdv-gui-amd64
  rm -f build/mdv-gui-arm64 build/mdv-gui-amd64
}

if [ "$GOOS" = "darwin" ]; then
  build_macos_universal "build/mdv-gui"
else
  go build -tags "production pdf_bundled" -ldflags "$LDFLAGS$WINGUI_LDFLAGS" -o "build/mdv-gui${HELPER_EXT}" ./gui
fi
macos_sign "build/mdv-gui${HELPER_EXT}"

echo "==> [3/4] Compressing GUI helper into launcher assets"
gzip -9 -c "build/mdv-gui${HELPER_EXT}" > internal/launcher/assets/mdv-gui.gz
ls -lh internal/launcher/assets/mdv-gui.gz

echo "==> [4/4] Compiling self-contained launcher"
if [ "$GOOS" = "darwin" ]; then
  CGO_ENABLED=0 GOARCH=arm64 go build -tags "gui_bundled pdf_bundled" -ldflags "$LDFLAGS" -o "build/mdv-arm64" ./cmd/mdv
  CGO_ENABLED=0 GOARCH=amd64 go build -tags "gui_bundled pdf_bundled" -ldflags "$LDFLAGS" -o "build/mdv-amd64" ./cmd/mdv
  lipo -create -output "build/mdv" build/mdv-arm64 build/mdv-amd64
  rm -f build/mdv-arm64 build/mdv-amd64
else
  go build -tags "gui_bundled pdf_bundled" -ldflags "$LDFLAGS$WINGUI_LDFLAGS" -o "build/mdv${HELPER_EXT}" ./cmd/mdv
fi
macos_sign "build/mdv${HELPER_EXT}"

# macOS: also produce mdv.app, a Finder-friendly bundle that lets users
# associate Markdown files with mdv (Open With / set as default app). It embeds
# the universal launcher just built above and is signed with the same identity.
if [ "$GOOS" = "darwin" ]; then
  echo "==> Building macOS app bundle (mdv.app)"
  scripts/make-macos-app.sh build/mdv.app build/mdv
fi

echo
echo "Done: build/mdv${HELPER_EXT}  (version ${VERSION}, ${GOOS}/${GOARCH})"
echo "Run:  ./build/mdv${HELPER_EXT} <file-or-folder>"
