# sign-windows.ps1 — AuthentiCode-sign one or more files with signtool.
#
# Reads the signing certificate from environment variables so the same script
# works during the build (to sign the embedded GUI helper) and afterwards (to
# sign the final launcher). It is a no-op when no certificate is configured, so
# local/dev builds are unaffected.
#
#   MDV_WINDOWS_SIGN_PFX        Path to the PKCS#12 (.pfx) code-signing cert.
#   MDV_WINDOWS_SIGN_PASSWORD   Password for the .pfx (optional).
#   MDV_WINDOWS_TIMESTAMP_URL   RFC3161 timestamp server (optional).
#
# Usage: pwsh scripts/sign-windows.ps1 <file> [<file> ...]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Files
)

$ErrorActionPreference = "Stop"

$pfx = $env:MDV_WINDOWS_SIGN_PFX
if ([string]::IsNullOrEmpty($pfx)) {
    # No certificate configured — nothing to sign.
    exit 0
}
if (-not (Test-Path $pfx)) {
    throw "MDV_WINDOWS_SIGN_PFX points to a missing file: $pfx"
}

$timestamp = $env:MDV_WINDOWS_TIMESTAMP_URL
if ([string]::IsNullOrEmpty($timestamp)) {
    $timestamp = "http://timestamp.digicert.com"
}

# Locate signtool.exe (not on PATH by default on GitHub runners).
function Get-SignTool {
    $cmd = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $roots = @(
        "${env:ProgramFiles(x86)}\Windows Kits\10\bin",
        "${env:ProgramFiles}\Windows Kits\10\bin"
    )
    foreach ($root in $roots) {
        if (Test-Path $root) {
            $found = Get-ChildItem -Path $root -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
                Where-Object { $_.FullName -match "x64" } |
                Sort-Object FullName -Descending |
                Select-Object -First 1
            if ($found) { return $found.FullName }
        }
    }
    throw "signtool.exe not found. Install the Windows 10/11 SDK."
}

$signtool = Get-SignTool

foreach ($file in $Files) {
    if ([string]::IsNullOrEmpty($file)) { continue }
    if (-not (Test-Path $file)) { throw "File to sign not found: $file" }
    Write-Host "==> AuthentiCode signing $file"
    $args = @("sign", "/fd", "SHA256", "/f", $pfx, "/tr", $timestamp, "/td", "SHA256")
    if (-not [string]::IsNullOrEmpty($env:MDV_WINDOWS_SIGN_PASSWORD)) {
        $args += @("/p", $env:MDV_WINDOWS_SIGN_PASSWORD)
    }
    $args += $file
    & $signtool @args
    if ($LASTEXITCODE -ne 0) { throw "signtool failed for $file (exit $LASTEXITCODE)" }
    & $signtool verify /pa $file | Out-Null
}
