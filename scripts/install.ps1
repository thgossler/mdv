<#
.SYNOPSIS
  Cross-platform mdv installer (PowerShell 7+). Downloads the latest
  self-contained mdv binary for the current OS/architecture from GitHub
  Releases and installs it to a directory on your PATH.

  Works on Windows, macOS, and Linux as long as PowerShell 7+ is installed.

.EXAMPLE
  # Windows
  irm https://raw.githubusercontent.com/thgossler/mdv/main/scripts/install.ps1 | iex

.EXAMPLE
  # macOS / Linux (with PowerShell 7+)
  pwsh -c "irm https://raw.githubusercontent.com/thgossler/mdv/main/scripts/install.ps1 | iex"

.NOTES
  Override the version with $env:MDV_VERSION and the install dir with
  $env:MDV_INSTALL before running.
#>
$ErrorActionPreference = "Stop"

# PowerShell 5.1 (Windows PowerShell) lacks the $IsWindows/$IsMacOS/$IsLinux
# automatic variables. Require PowerShell 7+ for reliable cross-platform behavior.
if ($PSVersionTable.PSVersion.Major -lt 6) {
    throw "This installer requires PowerShell 7 or newer. Download it from https://aka.ms/powershell"
}

$Repo = "thgossler/mdv"
$Version = if ($env:MDV_VERSION) { $env:MDV_VERSION } else { "latest" }

# --- Detect platform and pick the matching release asset ---------------------

function Get-Arch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "amd64" }
        "Arm64" { return "arm64" }
        default { throw "Unsupported architecture: $arch" }
    }
}

if ($IsWindows) {
    $arch = Get-Arch
    if ($arch -ne "amd64") { throw "No Windows build available for architecture '$arch'." }
    $Asset = "mdv-windows-amd64.exe"
    $IsArchive = $false
    $BinaryName = "mdv.exe"
}
elseif ($IsMacOS) {
    # macOS ships a single universal (arm64 + amd64) binary.
    $Asset = "mdv-darwin-universal.tar.gz"
    $IsArchive = $true
    $BinaryName = "mdv"
}
elseif ($IsLinux) {
    $arch = Get-Arch
    $Asset = "mdv-linux-$arch.tar.gz"
    $IsArchive = $true
    $BinaryName = "mdv"
}
else {
    throw "Unsupported operating system."
}

if ($Version -eq "latest") {
    $Url = "https://github.com/$Repo/releases/latest/download/$Asset"
} else {
    $Url = "https://github.com/$Repo/releases/download/$Version/$Asset"
}

# --- Determine the install directory -----------------------------------------

if ($env:MDV_INSTALL) {
    $InstallDir = $env:MDV_INSTALL
}
elseif ($IsWindows) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\mdv"
}
else {
    # Common per-user bin directory on macOS/Linux.
    $InstallDir = Join-Path $HOME ".local/bin"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$Dest = Join-Path $InstallDir $BinaryName

# --- Download ----------------------------------------------------------------

$platformLabel = if ($IsWindows) { "windows-amd64" } elseif ($IsMacOS) { "darwin-universal" } else { "linux-$arch" }
Write-Host "Downloading mdv ($platformLabel, $Version)…"

if ($IsArchive) {
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null
    $archivePath = Join-Path $tmp $Asset
    try {
        Invoke-WebRequest -Uri $Url -OutFile $archivePath -UseBasicParsing

        # `tar` ships with macOS, Linux, and Windows 10+ and handles .tar.gz.
        & tar -xzf $archivePath -C $tmp
        if ($LASTEXITCODE -ne 0) { throw "Failed to extract $Asset" }

        $extracted = Get-ChildItem -Path $tmp -Recurse -File |
            Where-Object { $_.Name -eq $BinaryName } |
            Select-Object -First 1
        if (-not $extracted) { throw "Could not find '$BinaryName' inside $Asset" }

        Copy-Item -Path $extracted.FullName -Destination $Dest -Force
    }
    finally {
        Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
else {
    Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing
}

# Ensure the binary is executable on Unix-like systems.
if (-not $IsWindows) {
    & chmod +x $Dest
}

Write-Host "Installed: $Dest"

# --- Make sure the install dir is on PATH ------------------------------------

if ($IsWindows) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($userPath -split ';') -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
        Write-Host "Added $InstallDir to your user PATH (persisted for new terminals)."
    }
}
else {
    # On macOS/Linux update the appropriate shell profile if the dir is missing.
    $onPath = ($env:PATH -split [IO.Path]::PathSeparator) -contains $InstallDir
    if (-not $onPath) {
        $shell = if ($env:SHELL) { Split-Path $env:SHELL -Leaf } else { "" }
        $profileFile = switch ($shell) {
            "zsh"  { Join-Path $HOME ".zshrc" }
            "bash" { if ($IsMacOS) { Join-Path $HOME ".bash_profile" } else { Join-Path $HOME ".bashrc" } }
            default { Join-Path $HOME ".profile" }
        }
        $exportLine = "export PATH=`"$InstallDir`:`$PATH`""
        $alreadyPresent = (Test-Path $profileFile) -and
            (Select-String -Path $profileFile -SimpleMatch $InstallDir -Quiet)
        if (-not $alreadyPresent) {
            Add-Content -Path $profileFile -Value "`n# Added by mdv installer`n$exportLine"
            Write-Host "Added $InstallDir to your PATH in $profileFile (applies to new shells)."
        }
    }
}

# Make mdv usable immediately in the current session. When this script is run
# via the documented `irm ... | iex` one-liner it executes in the current
# session scope, so updating $env:PATH takes effect right away — no terminal
# restart or profile reload needed.
$sep = [IO.Path]::PathSeparator
if (($env:PATH -split $sep) -notcontains $InstallDir) {
    $env:PATH = "$InstallDir$sep$env:PATH"
}

Write-Host "Try:  mdv --version"
