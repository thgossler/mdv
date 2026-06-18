#!/usr/bin/env bash
#
# notarize-macos.sh — submit a signed `mdv` artifact to Apple's notary service.
#
# Accepts either the bare `mdv` Mach-O executable or the `mdv.app` bundle. A
# bundle is stapled after notarization (the ticket travels with the archive);
# a bare executable cannot be stapled — `stapler` only supports bundles, disk
# images and installer packages — so Gatekeeper verifies it online on first run.
#
# No-op when the notary credentials are not configured (local/dev builds).
#
# Required env:
#   APPLE_ID         Apple ID email used for notarization.
#   APPLE_TEAM_ID    Developer Team ID (10 chars).
#   APPLE_APP_PASSWORD  App-specific password for the Apple ID.
#
# Usage: scripts/notarize-macos.sh [path]   (default: build/mdv)
set -euo pipefail

cd "$(dirname "$0")/.."
BIN="${1:-build/mdv}"

if [ -z "${APPLE_ID:-}" ] || [ -z "${APPLE_TEAM_ID:-}" ] || [ -z "${APPLE_APP_PASSWORD:-}" ]; then
  echo "notarize-macos: notary credentials not set — skipping notarization."
  exit 0
fi

if [ ! -e "$BIN" ]; then
  echo "notarize-macos: path not found: $BIN" >&2
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

# A .app bundle CAN be stapled, so the notarization ticket travels with the
# archive and Gatekeeper passes offline. A bare Mach-O executable cannot be
# stapled — Gatekeeper verifies it online on first run instead.
if [ -d "$BIN" ]; then
  echo "==> Stapling notarization ticket to bundle"
  xcrun stapler staple "$BIN"
  xcrun stapler validate "$BIN" || true
  spctl --assess --type execute --verbose=4 "$BIN" || true
else
  spctl --assess --type execute --verbose=4 "$BIN" || true
fi

rm -f "$ZIP"
echo "==> Done."
