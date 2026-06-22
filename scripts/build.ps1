# build.ps1 - produce a self-contained mdv.exe for Windows (x64).
#
# Mirrors scripts/build.sh: builds the Wails GUI frontend, compiles the GUI
# helper, gzips it into the launcher assets, then builds the launcher with the
# gui_bundled tag so the helper is embedded.
#
# Code signing is NOT done here. On Windows we sign with Azure Artifact Signing,
# which runs as a GitHub Action (see .github/workflows/release.yml). Because the
# GUI helper must be signed *before* it is gzipped into the launcher, the build
# is split into two stages so the signing action can run in between:
#
#   -Stage helper    build frontend + GUI helper -> build/mdv-gui.exe (then stop)
#   <sign helper>    (workflow step: azure/artifact-signing-action)
#   -Stage launcher  gzip helper + build launcher -> build/mdv.exe
#   <sign launcher>  (workflow step: azure/artifact-signing-action)
#
# Run with the default -Stage all for a normal one-shot local build.
#
# Usage: pwsh scripts/build.ps1 [-Version v1.2.3] [-Stage all|helper|launcher] [-Arch amd64|arm64]
param(
    [string]$Version = "",
    [ValidateSet("all", "helper", "launcher")]
    [string]$Stage = "all",
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64"
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

# Build for the requested architecture. On a native runner this matches the host,
# but we set it explicitly so the GUI helper (cgo) and launcher agree and the
# correct winres .syso is linked.
$env:GOOS = "windows"
$env:GOARCH = $Arch

if ([string]::IsNullOrEmpty($Version)) {
    $versionFile = Join-Path (Get-Location) "VERSION"
    if (Test-Path $versionFile) {
        $Version = "v" + ((Get-Content -Raw $versionFile).Trim())
    } else {
        try { $Version = (git describe --tags --always --dirty) } catch { $Version = "v0.0.0-dev" }
    }
}
$LdFlags = "-s -w -X github.com/thgossler/mdv/internal/core.Version=$Version"

# New-WinResSyso generates a Windows resource object so `go build` embeds the
# application icon (Explorer / desktop-shortcut / taskbar icon and, via Wails'
# resource-ID-3 lookup, the window / App-switcher / title-bar icon), an app
# manifest and version info. The generator lives in its own module so its
# build-only deps stay out of mdv's go.mod (see scripts/winres/main.go).
function New-WinResSyso {
    param(
        [Parameter(Mandatory)][string]$Package,
        [Parameter(Mandatory)][string]$Description
    )
    $root = (Get-Location).Path
    $ver = $Version.TrimStart('v')
    $out = Join-Path $root "$Package/rsrc_windows_$Arch.syso"
    $icon = Join-Path $root "images/icon.png"
    Push-Location (Join-Path $root "scripts/winres")
    try {
        go run . -icon $icon -out $out -version $ver -description $Description -arch $Arch
        if ($LASTEXITCODE -ne 0) { throw "winres syso generation failed for $Package" }
    } finally {
        Pop-Location
    }
}

New-Item -ItemType Directory -Force -Path build, internal/launcher/assets | Out-Null

if ($Stage -eq "all" -or $Stage -eq "helper") {
    Write-Host "==> [1/4] Building frontend"
    Push-Location gui/frontend
    if (-not (Test-Path node_modules)) { npm ci }
    npm run build
    Pop-Location

    # Safety net for the go:embed placeholder (see scripts/build.sh for the
    # rationale): guarantee dist/.gitkeep exists so `go vet` on the embed works.
    if (-not (Test-Path gui/frontend/dist/.gitkeep)) {
        Copy-Item gui/frontend/public/.gitkeep gui/frontend/dist/.gitkeep
    }

    # Keep the embedded GUI icon (window/App-switcher icon) in sync, and embed
    # the .exe resource icon for Explorer / shortcuts / taskbar.
    Copy-Item images/icon.png gui/appicon.png -Force
    New-WinResSyso -Package "gui" -Description "Markdown Viewer"

    Write-Host "==> [2/4] Generating bindings + compiling GUI helper"
    Push-Location gui
    & wails3 generate bindings -ts -i -clean=true 2>$null | Out-Null
    Pop-Location
    # -H windowsgui marks the PE as a GUI-subsystem binary so Windows never
    # allocates a console window for the helper (it is a webview app).
    $env:CGO_ENABLED = "1"
    go build -tags production -ldflags "$LdFlags -H windowsgui" -o build/mdv-gui.exe ./gui
}

if ($Stage -eq "helper") {
    Write-Host ""
    Write-Host "Helper built: build/mdv-gui.exe"
    Write-Host "Sign it, then run: pwsh scripts/build.ps1 -Stage launcher"
    return
}

if ($Stage -eq "all" -or $Stage -eq "launcher") {
    if (-not (Test-Path build/mdv-gui.exe)) {
        throw "build/mdv-gui.exe not found. Run with -Stage helper (or -Stage all) first."
    }

    Write-Host "==> [3/4] Compressing GUI helper into launcher assets"
    $in = [System.IO.File]::OpenRead("build/mdv-gui.exe")
    $out = [System.IO.File]::Create("internal/launcher/assets/mdv-gui.gz")
    $gzip = New-Object System.IO.Compression.GzipStream($out, [System.IO.Compression.CompressionLevel]::Optimal)
    $in.CopyTo($gzip)
    $gzip.Close(); $out.Close(); $in.Close()
    Get-Item internal/launcher/assets/mdv-gui.gz | Select-Object Length

    Write-Host "==> [4/4] Compiling self-contained launcher"
    New-WinResSyso -Package "cmd/mdv" -Description "Markdown Viewer"
    # -H windowsgui keeps Explorer from flashing a console window on a .md
    # double-click. The launcher reattaches to the parent console at startup
    # (see internal/console/console_windows.go) so terminal use still works.
    $env:CGO_ENABLED = "0"
    go build -tags gui_bundled -ldflags "$LdFlags -H windowsgui" -o build/mdv.exe ./cmd/mdv

    Write-Host ""
    Write-Host "Done: build/mdv.exe  (version $Version, windows/$Arch)"
    Write-Host "Run:  .\build\mdv.exe <file-or-folder>"
}
