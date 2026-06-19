#!/usr/bin/env bash
#
# bump-version.sh - bump the project's SemVer version.
#
# The canonical version lives in the repository-root VERSION file (e.g. 0.9.1).
# This script increments one component and resets the lower-level ones to 0,
# then keeps the Go default (internal/core/types.go) in sync so `go run` and
# unstamped builds report the same version.
#
# Usage:
#   scripts/bump-version.sh -Patch     # 0.9.1 -> 0.9.2
#   scripts/bump-version.sh -Minor     # 0.9.1 -> 0.10.0
#   scripts/bump-version.sh -Major     # 0.9.1 -> 1.0.0
#
# Long flags (--patch/--minor/--major) and lowercase are also accepted.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
VERSION_FILE="$ROOT/VERSION"
TYPES_FILE="$ROOT/internal/core/types.go"

usage() {
  echo "Usage: scripts/bump-version.sh -Patch | -Minor | -Major" >&2
  exit 2
}

[ $# -eq 1 ] || usage

part=""
case "$1" in
  -Patch|--patch|-patch|patch) part="patch" ;;
  -Minor|--minor|-minor|minor) part="minor" ;;
  -Major|--major|-major|major) part="major" ;;
  *) usage ;;
esac

[ -f "$VERSION_FILE" ] || { echo "bump-version: missing VERSION file at $VERSION_FILE" >&2; exit 1; }

current="$(tr -d ' \t\r\n' < "$VERSION_FILE")"
if ! printf '%s' "$current" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "bump-version: VERSION '$current' is not a valid MAJOR.MINOR.PATCH SemVer" >&2
  exit 1
fi

major="${current%%.*}"
rest="${current#*.}"
minor="${rest%%.*}"
patch="${rest#*.}"

case "$part" in
  patch) patch=$((patch + 1)) ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  major) major=$((major + 1)); minor=0; patch=0 ;;
esac

new="${major}.${minor}.${patch}"

printf '%s\n' "$new" > "$VERSION_FILE"

# Keep the Go default in sync: var Version = "vX.Y.Z"
if [ -f "$TYPES_FILE" ]; then
  tmp="$(mktemp)"
  sed -E "s/^(var Version = \")v[0-9]+\.[0-9]+\.[0-9]+(\")/\1v${new}\2/" "$TYPES_FILE" > "$tmp"
  mv "$tmp" "$TYPES_FILE"
fi

echo "Bumped version: ${current} -> ${new}"
echo "Updated: VERSION, internal/core/types.go"
