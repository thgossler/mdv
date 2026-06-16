#!/usr/bin/env sh
#
# mdv installer — downloads the latest self-contained `mdv` from GitHub Releases
# and installs it to a directory on your PATH. No package manager required.
#
#   curl -fsSL https://raw.githubusercontent.com/thgossler/mdv/main/scripts/install.sh | sh
#
# Environment overrides:
#   MDV_VERSION   install a specific tag (default: latest)
#   MDV_INSTALL   install directory (default: /usr/local/bin or ~/.local/bin)
set -eu

REPO="thgossler/mdv"
VERSION="${MDV_VERSION:-latest}"

# --- detect platform --------------------------------------------------------
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) target="darwin-universal" ;;
  Linux)
    case "$arch" in
      x86_64|amd64) target="linux-amd64" ;;
      aarch64|arm64) target="linux-arm64" ;;
      *) echo "mdv: unsupported Linux architecture: $arch" >&2; exit 1 ;;
    esac
    ;;
  *) echo "mdv: unsupported OS: $os (use install.ps1 on Windows)" >&2; exit 1 ;;
esac

asset="mdv-${target}.tar.gz"

# --- resolve download URL ---------------------------------------------------
if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

# --- choose install dir -----------------------------------------------------
if [ -n "${MDV_INSTALL:-}" ]; then
  bindir="$MDV_INSTALL"
elif [ -w "/usr/local/bin" ] 2>/dev/null; then
  bindir="/usr/local/bin"
else
  bindir="$HOME/.local/bin"
fi
mkdir -p "$bindir"

# --- download + extract -----------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading mdv ($target, $VERSION)…"
if command -v curl >/dev/null 2>&1; then
  curl -fSL "$url" -o "$tmp/$asset"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$tmp/$asset" "$url"
else
  echo "mdv: need curl or wget to download" >&2; exit 1
fi

tar -C "$tmp" -xzf "$tmp/$asset"
install -m 0755 "$tmp/mdv" "$bindir/mdv" 2>/dev/null || { cp "$tmp/mdv" "$bindir/mdv"; chmod 0755 "$bindir/mdv"; }

echo "Installed: $bindir/mdv"
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "Note: add $bindir to your PATH to run 'mdv' from anywhere." ;;
esac
echo "Try:  mdv --version"
