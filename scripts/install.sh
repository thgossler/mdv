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
on_path() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

writable_dir() {
  # Writable if the directory exists and is writable, or can be created.
  if [ -d "$1" ]; then
    [ -w "$1" ]
  else
    parent="$(dirname "$1")"
    [ -d "$parent" ] && [ -w "$parent" ]
  fi
}

if [ -n "${MDV_INSTALL:-}" ]; then
  bindir="$MDV_INSTALL"
else
  bindir=""
  # Prefer a directory that is already on PATH and writable, so `mdv` is
  # immediately runnable in the current shell — no profile reload or restart.
  for d in "/usr/local/bin" "$HOME/.local/bin" "$HOME/bin"; do
    if on_path "$d" && writable_dir "$d"; then
      bindir="$d"
      break
    fi
  done
  # Fall back to ~/.local/bin (created and added to PATH below).
  [ -n "$bindir" ] || bindir="$HOME/.local/bin"
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

# --- ensure mdv is usable right away ----------------------------------------
if on_path "$bindir"; then
  # Already discoverable in this shell; drop any stale command-lookup cache.
  hash -r 2>/dev/null || true
else
  # Persist for future shells by appending to the right shell profile.
  case "$(basename "${SHELL:-sh}")" in
    zsh)  profile="$HOME/.zshrc" ;;
    bash) [ "$os" = "Darwin" ] && profile="$HOME/.bash_profile" || profile="$HOME/.bashrc" ;;
    *)    profile="$HOME/.profile" ;;
  esac
  if [ ! -f "$profile" ] || ! grep -qF "$bindir" "$profile" 2>/dev/null; then
    printf '\n# Added by mdv installer\nexport PATH="%s:$PATH"\n' "$bindir" >> "$profile"
    echo "Added $bindir to PATH in $profile (applies to new shells)."
  fi
  # Make it available in this shell run too. This takes effect immediately when
  # the installer is sourced (e.g. '. install.sh'); when piped via 'curl | sh'
  # the child shell cannot change the parent, so also print the one-liner.
  export PATH="$bindir:$PATH"
  hash -r 2>/dev/null || true
  echo "To use mdv now in the current shell, run:  export PATH=\"$bindir:\$PATH\""
fi

echo "Try:  mdv --version"
