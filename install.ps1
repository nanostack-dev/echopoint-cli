<#
.SYNOPSIS
    Echopoint CLI installer for Windows (PowerShell).

.DESCRIPTION
    Downloads the latest echopoint release, verifies its SHA256 checksum,
    installs echopoint.exe into a per-user directory, and adds that directory
    to the user PATH. No administrator rights required.

.EXAMPLE
    irm https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.ps1 | iex

.PARAMETER Version
    Install a specific version (e.g. v0.3.0). Defaults to the latest release.

.PARAMETER Dir
    Install directory. Defaults to $env:LOCALAPPDATA\Programs\echopoint.

.PARAMETER NoModifyPath
    Do not add the install directory to the user PATH.
#>
[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$Dir = "",
    [switch]$NoModifyPath
)

$ErrorActionPreference = "Stop"
$Repo = "nanostack-dev/echopoint-cli"
$BinaryName = "echopoint.exe"

function Info  { param($m) Write-Host "[INFO] $m"  -ForegroundColor Green }
function Warn  { param($m) Write-Host "[WARN] $m"  -ForegroundColor Yellow }
function Fail  { param($m) Write-Host "[ERROR] $m" -ForegroundColor Red; exit 1 }

# Detect architecture (only amd64 is published for Windows).
$arch = $env:PROCESSOR_ARCHITECTURE
switch ($arch) {
    "AMD64" { $platform = "windows_amd64" }
    "x86"   { Fail "32-bit Windows is not supported" }
    "ARM64" { Fail "Windows on ARM64 is not supported" }
    default { Fail "Unsupported architecture: $arch" }
}
Info "Detected platform: $platform"

# Resolve install directory.
if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA "Programs\echopoint" }

# Resolve version.
if (-not $Version) {
    Info "Fetching latest version..."
    $latest = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
        -Headers @{ "User-Agent" = "echopoint-cli" }
    $Version = $latest.tag_name
    if (-not $Version) { Fail "Failed to determine latest version" }
}
Info "Version: $Version"

$versionNum = $Version.TrimStart("v")
$archiveName = "echopoint_${versionNum}_${platform}.zip"
$base = "https://github.com/$Repo/releases/download/$Version"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("echopoint-" + [System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    $archivePath = Join-Path $tmp $archiveName
    Info "Downloading $archiveName..."
    Invoke-WebRequest -Uri "$base/$archiveName" -OutFile $archivePath -Headers @{ "User-Agent" = "echopoint-cli" }

    # Verify checksum against checksums.txt.
    try {
        $checksumsPath = Join-Path $tmp "checksums.txt"
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $checksumsPath -Headers @{ "User-Agent" = "echopoint-cli" }
        $line = Select-String -Path $checksumsPath -Pattern ([regex]::Escape($archiveName)) | Select-Object -First 1
        if ($line) {
            $want = ($line.Line -split '\s+')[0].ToLower()
            $got = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()
            if ($got -ne $want) { Fail "checksum mismatch for $archiveName (got $got, want $want)" }
            Info "Checksum verified."
        } else {
            Warn "no checksum listed for $archiveName; skipping verification"
        }
    } catch {
        Warn "checksums.txt not available; skipping verification"
    }

    Info "Extracting..."
    Expand-Archive -Path $archivePath -DestinationPath $tmp -Force
    $binSrc = Join-Path $tmp $BinaryName
    if (-not (Test-Path $binSrc)) { Fail "binary not found in archive" }

    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    Copy-Item -Path $binSrc -Destination (Join-Path $Dir $BinaryName) -Force
    Info "Installed to $Dir\$BinaryName"
}
finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# Add to the user PATH (persistent) unless opted out.
if (-not $NoModifyPath) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$Dir*") {
        $newPath = if ($userPath) { "$userPath;$Dir" } else { $Dir }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = "$env:Path;$Dir"
        Info "Added $Dir to your user PATH (restart your terminal to pick it up)."
    }
}

Write-Host ""
Info "Installation complete!"
try {
    $v = & (Join-Path $Dir $BinaryName) version --short 2>$null
    Info "Installed version: $v"
} catch { }
Write-Host ""
Write-Host "Run 'echopoint --help' to get started."
Write-Host ""
