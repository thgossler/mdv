<#
.SYNOPSIS
  Bump the project's SemVer version.

.DESCRIPTION
  The canonical version lives in the repository-root VERSION file (e.g. 0.9.1).
  This script increments one component and resets the lower-level ones to 0,
  then keeps the Go default (internal/core/types.go) in sync so `go run` and
  unstamped builds report the same version.

.EXAMPLE
  pwsh scripts/bump-version.ps1 -Patch    # 0.9.1 -> 0.9.2
.EXAMPLE
  pwsh scripts/bump-version.ps1 -Minor    # 0.9.1 -> 0.10.0
.EXAMPLE
  pwsh scripts/bump-version.ps1 -Major    # 0.9.1 -> 1.0.0
#>
[CmdletBinding(DefaultParameterSetName = "Patch")]
param(
    [Parameter(ParameterSetName = "Patch")][switch]$Patch,
    [Parameter(ParameterSetName = "Minor")][switch]$Minor,
    [Parameter(ParameterSetName = "Major")][switch]$Major
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

$versionFile = Join-Path (Get-Location) "VERSION"
$typesFile = Join-Path (Get-Location) "internal/core/types.go"

if (-not (Test-Path $versionFile)) {
    throw "bump-version: missing VERSION file at $versionFile"
}

$current = (Get-Content -Raw $versionFile).Trim()
if ($current -notmatch '^\d+\.\d+\.\d+$') {
    throw "bump-version: VERSION '$current' is not a valid MAJOR.MINOR.PATCH SemVer"
}

$parts = $current.Split('.')
[int]$major = $parts[0]
[int]$minor = $parts[1]
[int]$patch = $parts[2]

switch ($PSCmdlet.ParameterSetName) {
    "Patch" { $patch++ }
    "Minor" { $minor++; $patch = 0 }
    "Major" { $major++; $minor = 0; $patch = 0 }
}

$new = "$major.$minor.$patch"

Set-Content -Path $versionFile -Value $new -NoNewline
Add-Content -Path $versionFile -Value "`n" -NoNewline

# Keep the Go default in sync: var Version = "vX.Y.Z"
if (Test-Path $typesFile) {
    $content = Get-Content -Raw $typesFile
    $content = [regex]::Replace(
        $content,
        '(var Version = ")v\d+\.\d+\.\d+(")',
        "`${1}v$new`$2")
    Set-Content -Path $typesFile -Value $content -NoNewline
}

Write-Host "Bumped version: $current -> $new"
Write-Host "Updated: VERSION, internal/core/types.go"
