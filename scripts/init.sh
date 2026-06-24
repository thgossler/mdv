#!/usr/bin/env bash
# init.sh – Check and install mdv build dependencies on macOS and Linux.
#
# Auto-installs:
#   Go 1.26+        brew (macOS) | official tarball (Linux, when brew absent)
#   Node.js 18+     brew (macOS) | NodeSource or system package manager (Linux)
#   git             brew (macOS) | system package manager (Linux)
#   C compiler      Xcode CLT (macOS, advisory) | gcc/build-essential (Linux)
#   wails3 CLI      go install github.com/wailsapp/wails/v3/cmd/wails3@latest
#
# Usage: bash scripts/init.sh

set -euo pipefail

cd "$(dirname "$0")/.."

MIN_GO_MAJOR=1; MIN_GO_MINOR=26
MIN_NODE_MAJOR=18

# Colour output (disabled when stdout is not a terminal).
if [ -t 1 ]; then
    BOLD='\033[1m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
    CYAN='\033[0;36m'; RESET='\033[0m'
else
    BOLD=''; GREEN=''; YELLOW=''; CYAN=''; RESET=''
fi

step() { printf "\n${BOLD}==> %s${RESET}\n" "$1"; }
good() { printf "    ${GREEN}OK     %s${RESET}\n" "$1"; }
warn() { printf "    ${YELLOW}WARN   %s${RESET}\n" "$1"; }
info() { printf "    -->    %s\n" "$1"; }

# ── Environment detection ─────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"

PKG_MGR=""
if   command -v brew    >/dev/null 2>&1; then PKG_MGR="brew"
elif command -v apt-get >/dev/null 2>&1; then PKG_MGR="apt"
elif command -v dnf     >/dev/null 2>&1; then PKG_MGR="dnf"
elif command -v yum     >/dev/null 2>&1; then PKG_MGR="yum"
elif command -v pacman  >/dev/null 2>&1; then PKG_MGR="pacman"
fi

# version_ge major1 minor1 req_major req_minor – true if v1.v2 >= req
version_ge() {
    [ "$1" -gt "$3" ] || { [ "$1" -eq "$3" ] && [ "$2" -ge "$4" ]; }
}

# ── Go install helpers ────────────────────────────────────────────────────────

# Download and install Go from the official tarball (Linux fallback).
install_go_tarball() {
    local go_arch=""
    case "$ARCH" in
        x86_64)  go_arch="amd64" ;;
        aarch64) go_arch="arm64" ;;
        armv*)   go_arch="armv6l" ;;
        *)
            warn "Unsupported architecture: $ARCH."
            info "Install Go manually: https://go.dev/dl/"
            return 1
            ;;
    esac

    # Fetch the current stable version string from go.dev.
    local go_ver
    go_ver="$(curl -fsSL 'https://go.dev/VERSION?m=text' 2>/dev/null | head -1 || echo 'go1.26.4')"
    go_ver="${go_ver#go}"   # strip leading "go" prefix

    local tarball="go${go_ver}.linux-${go_arch}.tar.gz"
    info "Downloading Go ${go_ver} (linux/${go_arch})..."
    curl -fsSL "https://go.dev/dl/${tarball}" -o "/tmp/${tarball}"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "/tmp/${tarball}"
    rm -f "/tmp/${tarball}"

    # Persist /usr/local/go/bin to ~/.profile for future shells.
    if ! grep -qF '/usr/local/go/bin' "${HOME}/.profile" 2>/dev/null; then
        echo 'export PATH="$PATH:/usr/local/go/bin"' >> "${HOME}/.profile"
        info "Added /usr/local/go/bin to ~/.profile"
    fi
    export PATH="$PATH:/usr/local/go/bin"
}

# ── Go ────────────────────────────────────────────────────────────────────────
step "Go (>= ${MIN_GO_MAJOR}.${MIN_GO_MINOR})"

GO_OK=false
if command -v go >/dev/null 2>&1; then
    GO_RAW="$(go version)"
    if [[ "$GO_RAW" =~ go([0-9]+)\.([0-9]+) ]]; then
        GO_MAJ="${BASH_REMATCH[1]}"; GO_MIN="${BASH_REMATCH[2]}"
        if version_ge "$GO_MAJ" "$GO_MIN" "$MIN_GO_MAJOR" "$MIN_GO_MINOR"; then
            good "$GO_RAW"
            GO_OK=true
        else
            info "Found Go ${GO_MAJ}.${GO_MIN} – need ${MIN_GO_MAJOR}.${MIN_GO_MINOR}+."
        fi
    fi
else
    info "Go not found."
fi

if [ "$GO_OK" = false ]; then
    info "Installing Go..."
    case "$OS" in
        Darwin)
            [ "$PKG_MGR" = "brew" ] || { warn "Homebrew not found. Install it first: https://brew.sh"; exit 1; }
            brew install go 2>/dev/null || brew upgrade go
            ;;
        Linux)
            case "$PKG_MGR" in
                brew)   brew install go 2>/dev/null || brew upgrade go ;;
                dnf)    sudo dnf  install -y golang || install_go_tarball ;;
                yum)    sudo yum  install -y golang || install_go_tarball ;;
                pacman) sudo pacman -S --noconfirm go ;;
                # apt's golang-go is often too old – prefer the official tarball.
                *)      install_go_tarball ;;
            esac
            ;;
        *)
            warn "Unsupported OS: $OS. Install Go manually: https://go.dev/dl/"
            exit 1
            ;;
    esac
    good "$(go version)"
fi

# ── Node.js ──────────────────────────────────────────────────────────────────
step "Node.js (>= ${MIN_NODE_MAJOR})"

NODE_OK=false
if command -v node >/dev/null 2>&1; then
    NODE_RAW="$(node --version)"
    if [[ "$NODE_RAW" =~ ^v([0-9]+) ]]; then
        NODE_MAJ="${BASH_REMATCH[1]}"
        if [ "$NODE_MAJ" -ge "$MIN_NODE_MAJOR" ]; then
            NPM_RAW="$(npm --version 2>/dev/null || echo '?')"
            good "node $NODE_RAW  npm $NPM_RAW"
            NODE_OK=true
        else
            info "Found Node.js $NODE_RAW – need v${MIN_NODE_MAJOR}+."
        fi
    fi
else
    info "Node.js not found."
fi

if [ "$NODE_OK" = false ]; then
    info "Installing Node.js LTS..."
    case "$OS" in
        Darwin)
            [ "$PKG_MGR" = "brew" ] || { warn "Homebrew not found. Install it first: https://brew.sh"; exit 1; }
            brew install node 2>/dev/null || brew upgrade node
            ;;
        Linux)
            case "$PKG_MGR" in
                brew)   brew install node 2>/dev/null || brew upgrade node ;;
                apt)
                    info "Setting up NodeSource LTS repository..."
                    curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash -
                    sudo apt-get install -y nodejs
                    ;;
                dnf)
                    curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash -
                    sudo dnf install -y nodejs
                    ;;
                yum)
                    curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash -
                    sudo yum install -y nodejs
                    ;;
                pacman)
                    sudo pacman -S --noconfirm nodejs npm
                    ;;
                *)
                    warn "No supported package manager found."
                    info "Install Node.js manually: https://nodejs.org/en/download"
                    exit 1
                    ;;
            esac
            ;;
        *)
            warn "Unsupported OS: $OS. Install Node.js manually: https://nodejs.org"
            exit 1
            ;;
    esac
    good "node $(node --version)  npm $(npm --version)"
fi

# ── git ───────────────────────────────────────────────────────────────────────
step "git"

if command -v git >/dev/null 2>&1; then
    good "$(git --version)"
else
    info "git not found. Installing."
    case "$OS" in
        Darwin)
            # git is bundled with Xcode CLT; trigger installation if absent.
            if [ "$PKG_MGR" = "brew" ]; then
                brew install git
            else
                xcode-select --install 2>/dev/null || true
                warn "Run 'xcode-select --install' in a terminal if git is still missing after the dialog."
            fi
            ;;
        Linux)
            case "$PKG_MGR" in
                brew)   brew install git ;;
                apt)    sudo apt-get install -y git ;;
                dnf)    sudo dnf  install -y git ;;
                yum)    sudo yum  install -y git ;;
                pacman) sudo pacman -S --noconfirm git ;;
                *)      warn "Install git manually: https://git-scm.com" ;;
            esac
            ;;
    esac
    command -v git >/dev/null 2>&1 && good "$(git --version)" || true
fi

# ── wails3 CLI ───────────────────────────────────────────────────────────────
step "wails3 CLI"

# Ensure $(go env GOPATH)/bin is on PATH so go-installed tools are visible.
GOPATH_BIN="$(go env GOPATH)/bin"
if [[ ":$PATH:" != *":${GOPATH_BIN}:"* ]]; then
    export PATH="$PATH:${GOPATH_BIN}"
fi

WAILS_OK=false
if command -v wails3 >/dev/null 2>&1; then
    WAILS_RAW="$(wails3 version 2>/dev/null || true)"
    if [[ "$WAILS_RAW" =~ ^v[0-9] ]]; then
        good "wails3 $WAILS_RAW"
        WAILS_OK=true
    fi
fi

if [ "$WAILS_OK" = false ]; then
    info "Installing wails3 via go install..."
    go install github.com/wailsapp/wails/v3/cmd/wails3@latest

    # Persist GOPATH/bin to ~/.profile for future shells if not already there.
    if ! grep -qF "${GOPATH_BIN}" "${HOME}/.profile" 2>/dev/null; then
        echo "export PATH=\"\$PATH:${GOPATH_BIN}\"" >> "${HOME}/.profile"
        info "Added ${GOPATH_BIN} to ~/.profile"
    fi

    good "wails3 $(wails3 version)"
fi

# ── C compiler (CGO) ──────────────────────────────────────────────────────────
step "C compiler (CGO – required for GUI helper)"

C_OK=false
if command -v gcc   >/dev/null 2>&1; then good "$(gcc   --version 2>&1 | head -1)"; C_OK=true
elif command -v clang >/dev/null 2>&1; then good "$(clang --version 2>&1 | head -1)"; C_OK=true
fi

if [ "$C_OK" = false ]; then
    info "C compiler not found. Installing..."
    case "$OS" in
        Darwin)
            # Xcode CLT installation shows a GUI dialog; guide the user.
            warn "Xcode Command Line Tools not found."
            info "Run in a terminal and follow the on-screen dialog:"
            info "  xcode-select --install"
            ;;
        Linux)
            case "$PKG_MGR" in
                brew)   brew install gcc 2>/dev/null || brew upgrade gcc ;;
                apt)    sudo apt-get install -y build-essential ;;
                dnf)    sudo dnf  install -y gcc ;;
                yum)    sudo yum  install -y gcc ;;
                pacman) sudo pacman -S --noconfirm base-devel ;;
                *)      warn "Install gcc manually (e.g. 'sudo apt-get install build-essential')." ;;
            esac
            command -v gcc >/dev/null 2>&1 && good "$(gcc --version 2>&1 | head -1)" || true
            ;;
    esac
fi

# ── Done ──────────────────────────────────────────────────────────────────────
printf "\n${CYAN}Dependency check complete.${RESET}\n"
printf "${CYAN}You may need to open a new shell for PATH changes to take full effect.${RESET}\n"
printf "${CYAN}Then build with: bash scripts/build.sh${RESET}\n"
