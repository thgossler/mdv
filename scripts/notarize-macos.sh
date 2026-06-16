#!/usr/bin/env bash
#
# notarize-macos.sh — submit the signed `mdv` binary to Apple's notary service.
#
# The launcher is a single Mach-O executable (not an .app/.pkg/.dmg), so a
# notarization *ticket* cannot be stapled to it — `stapler` only supports
# bundles, disk images and installer packages. Notarization still registers the
# binary with Apple so Gatekeeper passes its online check on first launch.
#
# No-op when the notary credentials are not configured (local/dev builds).
#
# Required env:
#   APPLE_ID         Apple ID email used for notarization.
#   APPLE_TEAM_ID    Developer Team ID (10 chars).
#   APPLE_APP_PASSWORD  App-specific password for the Apple ID.
#
# Usage: scripts/notarize-macos.sh [path-to-binary]   (default: build/mdv)
set -euo pipefail

cd "$(dirname "$0")/.."
BIN="${1:-build/mdv}"

if [ -z "${APPLE_ID:-}" ] || [ -z "${APPLE_TEAM_ID:-}" ] || [ -z "${APPLE_APP_PASSWORD:-}" ]; then
  echo "notarize-macos: notary credentials not set — skipping notarization."
  exit 0
fi

if [ ! -f "$BIN" ]; then
  echo "notarize-macos: binary not found: $BIN" >&2
  exit 1
fi

ZIP="build/$(basename "$BIN")-notarize.zip"
echo "==> Zipping $BIN for notarization"
ditto -c -k --keepParent "$BIN" "$ZIP"

echo "==> Submitting to Apple notary service (this can take a few minutes)"
xcrun notarytool submit "$ZIP" \
  --apple-id "$APPLE_ID" \
  --team-id "$APPLE_TEAM_ID" \
  --password "$APPLE_APP_PASSWORD" \
  --wait

echo "==> Notarization complete. Verifying signature/Gatekeeper assessment:"
codesign --verify --strict --verbose=2 "$BIN" || true
spctl --assess --type execute --verbose=4 "$BIN" || true

rm -f "$ZIP"
echo "==> Done. Note: a bare executable cannot be stapled; Gatekeeper verifies"
echo "    the notarization online on first run."
