# init.ps1 – Check and install mdv build dependencies on Windows.
#
# Auto-installs (using winget or go install):
#   Go 1.26+        winget  GoLang.Go
#   Node.js 18+     winget  OpenJS.NodeJS.LTS
#   git             winget  Git.Git
#   wails3 CLI      go install github.com/wailsapp/wails/v3/cmd/wails3@latest
#
# Also checks and advises on:
#   C compiler      gcc (MinGW-w64) or cl.exe (MSVC) – required for CGO
#
# Requires Windows 10 1809+ with winget (App Installer from the Microsoft Store).
# Usage: pwsh scripts/init.ps1

$ErrorActionPreference = "Stop"

$MIN_GO_MAJOR   = 1; $MIN_GO_MINOR   = 26
$MIN_NODE_MAJOR = 18

function step { param([string]$m) Write-Host "`n==> $m" }
function good { param([string]$m) Write-Host "    OK     $m" -ForegroundColor Green }
function warn { param([string]$m) Write-Host "    WARN   $m" -ForegroundColor Yellow }
function info { param([string]$m) Write-Host "    -->    $m" }

# Reload PATH from the registry so tools installed moments ago become visible.
function Update-SessionPath {
    $mp = [System.Environment]::GetEnvironmentVariable("PATH", "Machine")
    $up = [System.Environment]::GetEnvironmentVariable("PATH", "User")
    $env:PATH = "$mp;$up"
}

function Assert-Winget {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
        throw "winget is not available. Install 'App Installer' from the Microsoft Store and re-run."
    }
}

# ── Go ────────────────────────────────────────────────────────────────────────
step "Go (>= $MIN_GO_MAJOR.$MIN_GO_MINOR)"
Update-SessionPath

$goOk  = $false
$goRaw = try { (go version 2>&1).ToString() } catch { "" }
if ($goRaw -match 'go(\d+)\.(\d+)') {
    $maj = [int]$Matches[1]; $min = [int]$Matches[2]
    if ($maj -gt $MIN_GO_MAJOR -or ($maj -eq $MIN_GO_MAJOR -and $min -ge $MIN_GO_MINOR)) {
        good $goRaw.Trim()
        $goOk = $true
    } else {
        info "Found Go $maj.$min – need $MIN_GO_MAJOR.$MIN_GO_MINOR+. Upgrading."
    }
} else {
    info "Go not found. Installing."
}

if (-not $goOk) {
    Assert-Winget
    winget install --id GoLang.Go --accept-source-agreements --accept-package-agreements
    Update-SessionPath
    good ((go version 2>&1).ToString().Trim())
}

# ── Node.js ──────────────────────────────────────────────────────────────────
step "Node.js (>= $MIN_NODE_MAJOR)"
Update-SessionPath

$nodeOk  = $false
$nodeRaw = try { (node --version 2>&1).ToString().Trim() } catch { "" }
if ($nodeRaw -match '^v(\d+)') {
    $nodeMaj = [int]$Matches[1]
    if ($nodeMaj -ge $MIN_NODE_MAJOR) {
        $npmRaw = try { (npm --version 2>&1).ToString().Trim() } catch { "?" }
        good "node $nodeRaw  npm $npmRaw"
        $nodeOk = $true
    } else {
        info "Found Node.js $nodeRaw – need v$MIN_NODE_MAJOR+. Installing LTS."
    }
} else {
    info "Node.js not found. Installing LTS."
}

if (-not $nodeOk) {
    Assert-Winget
    winget install --id OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements
    Update-SessionPath
    $nodeVer = (node --version 2>&1).ToString().Trim()
    $npmVer  = (npm  --version 2>&1).ToString().Trim()
    good "node $nodeVer  npm $npmVer"
}

# ── git ───────────────────────────────────────────────────────────────────────
step "git"
Update-SessionPath

$gitRaw = try { (git --version 2>&1).ToString().Trim() } catch { "" }
if ($gitRaw -match 'git version') {
    good $gitRaw
} else {
    info "git not found. Installing."
    Assert-Winget
    winget install --id Git.Git --accept-source-agreements --accept-package-agreements
    Update-SessionPath
    good ((git --version 2>&1).ToString().Trim())
}

# ── wails3 CLI ───────────────────────────────────────────────────────────────
step "wails3 CLI"
Update-SessionPath

# Ensure the Go bin directory is on PATH so go-installed tools are visible.
$goBin = try { (go env GOPATH 2>&1).ToString().Trim() + "\bin" } catch { "" }
if ($goBin -and $env:PATH -notlike "*$goBin*") {
    $env:PATH = "$env:PATH;$goBin"
}

$wailsOk  = $false
$wailsRaw = try { (wails3 version 2>&1).ToString().Trim() } catch { "" }
if ($wailsRaw -match '^v\d+') {
    good "wails3 $wailsRaw"
    $wailsOk = $true
} else {
    info "wails3 not found. Installing via go install."
}

if (-not $wailsOk) {
    go install github.com/wailsapp/wails/v3/cmd/wails3@latest
    if ($LASTEXITCODE -ne 0) { throw "go install wails3 failed." }
    Update-SessionPath
    if ($goBin -and $env:PATH -notlike "*$goBin*") { $env:PATH = "$env:PATH;$goBin" }
    good "wails3 $((wails3 version 2>&1).ToString().Trim())"
}

# ── C compiler (CGO) ──────────────────────────────────────────────────────────
step "C compiler (CGO – required for GUI helper)"
Update-SessionPath

$cOk = $false

# Check gcc (MinGW-w64 / MSYS2 / Cygwin).
$gccRaw = try { (gcc --version 2>&1).ToString() } catch { "" }
if ($gccRaw -match 'gcc') {
    good ($gccRaw -split "`r?`n")[0].Trim()
    $cOk = $true
}

if (-not $cOk) {
    # cl.exe is never in the regular PATH; use vswhere.exe (shipped with every
    # VS / Build Tools install) to detect a C++ workload instead. The Go
    # toolchain locates cl.exe the same way, so if vswhere reports it, CGO works.
    $vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
    if (Test-Path $vswhere) {
        $vsPath = try {
            & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 `
                       -property installationPath 2>$null
        } catch { "" }
        if ($vsPath) {
            good "MSVC (Visual Studio Build Tools) at $vsPath"
            $cOk = $true
        }
    }
}

if (-not $cOk) {
    warn "No C compiler found. CGO requires gcc or MSVC to build the GUI helper."
    info "Option A – MinGW-w64 via MSYS2:"
    info "  1. winget install --id MSYS2.MSYS2"
    info "  2. Open the MSYS2 UCRT64 shell and run:"
    info "       pacman -S mingw-w64-ucrt-x86_64-gcc"
    info "  3. Add C:\msys64\ucrt64\bin to your PATH."
    info "Option B – Visual Studio Build Tools (select 'Desktop development with C++'):"
    info "  https://aka.ms/vs/buildtools"
}

# ── Done ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "Dependency check complete." -ForegroundColor Cyan
Write-Host "Open a new terminal for PATH changes to take full effect, then build:" -ForegroundColor Cyan
Write-Host "  pwsh scripts/build.ps1" -ForegroundColor Cyan
