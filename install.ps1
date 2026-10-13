# save-token installer for Windows (PowerShell)
#
# Usage:
#   irm https://raw.githubusercontent.com/Nag112/save-token/main/install.ps1 | iex
#
# Installs save-token.exe to $HOME\.local\bin (override with
# $env:SAVE_TOKEN_INSTALL_DIR). Adds it to your user PATH if needed.

$ErrorActionPreference = "Stop"

$Repo = "Nag112/save-token"
$Version = if ($env:SAVE_TOKEN_VERSION) { $env:SAVE_TOKEN_VERSION } else { "0.1.0" }
$InstallDir = if ($env:SAVE_TOKEN_INSTALL_DIR) { $env:SAVE_TOKEN_INSTALL_DIR } else { Join-Path $HOME ".local\bin" }

function Info($msg) { Write-Host "[save-token] $msg" }
function Die($msg) { Write-Host "[save-token] error: $msg" -ForegroundColor Red; exit 1 }

# --- Platform detection (release assets: save-token-<version>-windows-<arch>.zip)
$Arch = switch (${env:PROCESSOR_ARCHITECTURE}) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { Die "unsupported architecture: ${env:PROCESSOR_ARCHITECTURE}" }
}

$Asset = "save-token-$Version-windows-$Arch.zip"
$Url = "https://github.com/$Repo/releases/download/v$Version/$Asset"
$Target = Join-Path $InstallDir "save-token.exe"

Info "Downloading $Url"
$Tmp = New-Item -ItemType Directory -Path ([System.IO.Path]::GetRandomFileName()) -Confirm:$false
try {
    $Zip = Join-Path $Tmp.FullName $Asset
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Zip -UseBasicParsing
    } catch {
        Die "download failed: $Url"
    }

    Expand-Archive -Path $Zip -DestinationPath $Tmp.FullName -Force
    $Bin = Join-Path $Tmp.FullName "save-token.exe"
    if (-not (Test-Path $Bin)) { Die "archive did not contain a save-token.exe binary" }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Move-Item -Force $Bin $Target
} finally {
    Remove-Item -Recurse -Force $Tmp.FullName -ErrorAction SilentlyContinue
}

$ver = & $Target --version 2>$null
Info "Installed $ver to $Target"

# --- PATH check (user scope)
$PathDirs = [Environment]::GetEnvironmentVariable("Path", "User") -split ";"
if ($PathDirs -notcontains $InstallDir) {
    Info "$InstallDir is not on your user PATH."
    try {
        $newPath = ($PathDirs + $InstallDir) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        Info "Added $InstallDir to your user PATH. Restart your terminal to pick it up."
    } catch {
        Info "Add it manually: setx PATH \"%PATH%;$InstallDir\""
    }
}

Info "Done. Try it:"
Write-Host "    save-token git status"
Write-Host "    save-token --dry-run pytest"
