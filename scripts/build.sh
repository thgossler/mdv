#!/usr/bin/env bash
#
# build.sh — produce a self-contained `mdv` executable for the host platform.
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
VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo v0.0.0-dev)}"
LDFLAGS="-s -w -X github.com/thgossler/mdv/internal/core.Version=${VERSION}"

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
HELPER_EXT=""
[ "$GOOS" = "windows" ] && HELPER_EXT=".exe"

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

echo "==> [1/4] Building frontend"
pushd gui/frontend >/dev/null
if [ ! -d node_modules ]; then
  npm ci || npm install
fi
npm run build
popd >/dev/null

echo "==> [2/4] Generating bindings + compiling GUI helper"
( cd gui && wails3 generate bindings -ts -i -clean=true >/dev/null 2>&1 || true )

build_macos_universal() {
  # Build the GUI helper as a universal (arm64 + amd64) binary.
  local out_helper="$1"
  echo "    macOS universal: building arm64 + amd64 slices"
  CGO_ENABLED=1 GOARCH=arm64 go build -tags production -ldflags "$LDFLAGS" -o "build/mdv-gui-arm64" ./gui
  CGO_ENABLED=1 GOARCH=amd64 go build -tags production -ldflags "$LDFLAGS" -o "build/mdv-gui-amd64" ./gui
  lipo -create -output "$out_helper" build/mdv-gui-arm64 build/mdv-gui-amd64
  rm -f build/mdv-gui-arm64 build/mdv-gui-amd64
}

if [ "$GOOS" = "darwin" ]; then
  build_macos_universal "build/mdv-gui"
else
  go build -tags production -ldflags "$LDFLAGS" -o "build/mdv-gui${HELPER_EXT}" ./gui
fi
macos_sign "build/mdv-gui${HELPER_EXT}"

echo "==> [3/4] Compressing GUI helper into launcher assets"
gzip -9 -c "build/mdv-gui${HELPER_EXT}" > internal/launcher/assets/mdv-gui.gz
ls -lh internal/launcher/assets/mdv-gui.gz

echo "==> [4/4] Compiling self-contained launcher"
if [ "$GOOS" = "darwin" ]; then
  CGO_ENABLED=0 GOARCH=arm64 go build -tags gui_bundled -ldflags "$LDFLAGS" -o "build/mdv-arm64" ./cmd/mdv
  CGO_ENABLED=0 GOARCH=amd64 go build -tags gui_bundled -ldflags "$LDFLAGS" -o "build/mdv-amd64" ./cmd/mdv
  lipo -create -output "build/mdv" build/mdv-arm64 build/mdv-amd64
  rm -f build/mdv-arm64 build/mdv-amd64
else
  go build -tags gui_bundled -ldflags "$LDFLAGS" -o "build/mdv${HELPER_EXT}" ./cmd/mdv
fi
macos_sign "build/mdv${HELPER_EXT}"

echo
echo "Done: build/mdv${HELPER_EXT}  (version ${VERSION}, ${GOOS}/${GOARCH})"
echo "Run:  ./build/mdv${HELPER_EXT} <file-or-folder>"
