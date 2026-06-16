# build.ps1 — produce a self-contained mdv.exe for Windows (x64).
#
# Mirrors scripts/build.sh: builds the Wails GUI frontend, compiles the GUI
# helper, gzips it into the launcher assets, then builds the launcher with the
# gui_bundled tag so the helper is embedded.
#
# Usage: pwsh scripts/build.ps1 [-Version v1.2.3]
param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

if ([string]::IsNullOrEmpty($Version)) {
    try { $Version = (git describe --tags --always --dirty) } catch { $Version = "v0.0.0-dev" }
}
$LdFlags = "-s -w -X github.com/thgossler/mdv/internal/core.Version=$Version"

New-Item -ItemType Directory -Force -Path build, internal/launcher/assets | Out-Null

Write-Host "==> [1/4] Building frontend"
Push-Location gui/frontend
if (-not (Test-Path node_modules)) { npm ci }
npm run build
Pop-Location

Write-Host "==> [2/4] Generating bindings + compiling GUI helper"
Push-Location gui
& wails3 generate bindings -ts -i -clean=true 2>$null | Out-Null
Pop-Location
$env:CGO_ENABLED = "1"
go build -tags production -ldflags $LdFlags -o build/mdv-gui.exe ./gui

Write-Host "==> [3/4] Compressing GUI helper into launcher assets"
$in = [System.IO.File]::OpenRead("build/mdv-gui.exe")
$out = [System.IO.File]::Create("internal/launcher/assets/mdv-gui.gz")
$gzip = New-Object System.IO.Compression.GzipStream($out, [System.IO.Compression.CompressionLevel]::Optimal)
$in.CopyTo($gzip)
$gzip.Close(); $out.Close(); $in.Close()
Get-Item internal/launcher/assets/mdv-gui.gz | Select-Object Length

Write-Host "==> [4/4] Compiling self-contained launcher"
$env:CGO_ENABLED = "0"
go build -tags gui_bundled -ldflags $LdFlags -o build/mdv.exe ./cmd/mdv

Write-Host ""
Write-Host "Done: build/mdv.exe  (version $Version, windows/amd64)"
Write-Host "Run:  .\build\mdv.exe <file-or-folder>"
