<#
.SYNOPSIS
  mdv installer for Windows. Downloads the latest self-contained mdv.exe from
  GitHub Releases and installs it to a directory on your PATH.

.EXAMPLE
  irm https://raw.githubusercontent.com/thgossler/mdv/main/scripts/install.ps1 | iex

.NOTES
  Override the version with $env:MDV_VERSION and the install dir with
  $env:MDV_INSTALL before running.
#>
$ErrorActionPreference = "Stop"

$Repo = "thgossler/mdv"
$Version = if ($env:MDV_VERSION) { $env:MDV_VERSION } else { "latest" }
$Asset = "mdv-windows-amd64.exe"

if ($Version -eq "latest") {
    $Url = "https://github.com/$Repo/releases/latest/download/$Asset"
} else {
    $Url = "https://github.com/$Repo/releases/download/$Version/$Asset"
}

$InstallDir = if ($env:MDV_INSTALL) { $env:MDV_INSTALL } else { Join-Path $env:LOCALAPPDATA "Programs\mdv" }
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$Dest = Join-Path $InstallDir "mdv.exe"

Write-Host "Downloading mdv (windows-amd64, $Version)…"
Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing

Write-Host "Installed: $Dest"

# Add to the user PATH if missing.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    Write-Host "Added $InstallDir to your user PATH (restart your terminal to use 'mdv')."
}
Write-Host "Try:  mdv --version"
