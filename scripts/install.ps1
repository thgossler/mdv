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
  $env:MDV_INSTALL before running. Set $env:MDV_ASSOCIATE_MD to 1/yes (or no) to
  associate .md files with mdv without being prompted.

  Use -Silent for unattended installs (no prompts; file association is left
  untouched by default), and -AssociateMdFileExtension to associate .md files
  with mdv even in -Silent mode - handy when chaining this script from another
  installer or tool.
#>
param(
    # Run unattended: never prompt. Without -AssociateMdFileExtension no file
    # association is performed.
    [switch]$Silent,
    # Associate the .md file extension with mdv (honored even with -Silent).
    [switch]$AssociateMdFileExtension
)
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
        "X64"   { return "x64" }
        "Arm64" { return "arm64" }
        default { throw "Unsupported architecture: $arch" }
    }
}

if ($IsWindows) {
    $arch = Get-Arch
    if ($arch -notin @("x64", "arm64")) { throw "No Windows build available for architecture '$arch'." }
    $Asset = "mdv-windows-$arch.zip"
    $IsArchive = $true
    $BinaryName = "mdv.exe"
}
elseif ($IsMacOS) {
    # macOS ships a single universal (arm64 + amd64) binary.
    $Asset = "mdv-macos-darwin-universal.tar.gz"
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

$platformLabel = if ($IsWindows) { "windows-$arch" } elseif ($IsMacOS) { "macos-darwin-universal" } else { "linux-$arch" }
Write-Host "Downloading mdv ($platformLabel, $Version)…"

if ($IsArchive) {
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null
    $archivePath = Join-Path $tmp $Asset
    try {
        Invoke-WebRequest -Uri $Url -OutFile $archivePath -UseBasicParsing

        # `tar` ships with macOS, Linux, and Windows 10+ and auto-detects the
        # compression, so it handles both .tar.gz and .zip archives.
        & tar -xf $archivePath -C $tmp
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
# session scope, so updating $env:PATH takes effect right away - no terminal
# restart or profile reload needed.
$sep = [IO.Path]::PathSeparator
if (($env:PATH -split $sep) -notcontains $InstallDir) {
    $env:PATH = "$InstallDir$sep$env:PATH"
}

# --- Optionally associate .md files with mdv (Windows) -----------------------
# Registers a per-user ProgID and points the .md extension at it. No admin
# rights needed. Set $env:MDV_ASSOCIATE_MD to 1/yes to associate without
# prompting, or to no to skip silently (useful for non-interactive installs).

function Set-MdvMarkdownAssociation {
    param([Parameter(Mandatory)][string]$ExePath)

    $progId  = "mdv.Markdown"
    $classes = "HKCU:\Software\Classes"

    # ProgID: friendly name, document icon, and the command used to open a file.
    New-Item -Path "$classes\$progId\shell\open\command" -Force | Out-Null
    Set-ItemProperty -Path "$classes\$progId" -Name "(default)" -Value "Markdown Document"
    New-Item -Path "$classes\$progId\DefaultIcon" -Force | Out-Null
    Set-ItemProperty -Path "$classes\$progId\DefaultIcon" -Name "(default)" -Value "$ExePath,0"
    Set-ItemProperty -Path "$classes\$progId\shell\open\command" -Name "(default)" -Value "`"$ExePath`" --gui `"%1`""

    # List the ProgID under .md's "Open with" set and make it the per-user default.
    New-Item -Path "$classes\.md\OpenWithProgids" -Force | Out-Null
    New-ItemProperty -Path "$classes\.md\OpenWithProgids" -Name $progId -PropertyType String -Value "" -Force | Out-Null
    Set-ItemProperty -Path "$classes\.md" -Name "(default)" -Value $progId

    # Notify the shell so the new association is picked up without a sign-out.
    try {
        Add-Type -Namespace MdvInstaller -Name Shell -MemberDefinition '[DllImport("shell32.dll")] public static extern void SHChangeNotify(int eventId, int flags, System.IntPtr item1, System.IntPtr item2);' -ErrorAction SilentlyContinue
        [MdvInstaller.Shell]::SHChangeNotify(0x08000000, 0, [IntPtr]::Zero, [IntPtr]::Zero)
    } catch { }

    Write-Host "Associated .md files with mdv for the current user."
    Write-Host "If Windows keeps a previous default, set it via Settings > Apps > Default apps,"
    Write-Host "or right-click a .md file > Open with > Choose another app > mdv > Always."
}

if ($IsWindows) {
    $doAssoc = $false
    if ($AssociateMdFileExtension -or ($env:MDV_ASSOCIATE_MD -match '^(1|y|yes|true)$')) {
        $doAssoc = $true
    }
    elseif (-not $Silent -and [Environment]::UserInteractive -and -not [Console]::IsInputRedirected) {
        $reply = Read-Host "Associate Markdown (.md) files with mdv? [y/N]"
        $doAssoc = $reply -match '^\s*(y|yes)\s*$'
    }
    if ($doAssoc) {
        try { Set-MdvMarkdownAssociation -ExePath $Dest }
        catch { Write-Warning "Could not set .md association: $($_.Exception.Message)" }
    }
}

Write-Host "Try:  mdv --version"
