param(
  [string]$BinaryUrl = $env:CLIP_SERVER_BINARY_URL,
  [string]$BaseUrl = $(if ($env:CLIP_SERVER_BASE_URL) { $env:CLIP_SERVER_BASE_URL } else { "https://guajun.github.io/ClipboardFileServer" }),
  [string]$InstallDir = $(if ($env:CLIP_SERVER_INSTALL_DIR) { $env:CLIP_SERVER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\clip-server" }),
  [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"

function Join-ClipUrl {
  param(
    [string]$Root,
    [string]$File
  )
  return "$($Root.TrimEnd('/'))/$File"
}

function Get-ClipServerWindowsArch {
  $values = @($env:PROCESSOR_ARCHITEW6432, $env:PROCESSOR_ARCHITECTURE) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
  $archText = ($values -join ' ').ToLowerInvariant()
  if ($archText -match 'arm64|aarch64') {
    return "arm64"
  }
  if ($archText -match 'amd64|x64|x86_64') {
    return "amd64"
  }
  throw "Unsupported Windows architecture: $archText. Supported architectures: amd64, arm64."
}

if ([string]::IsNullOrWhiteSpace($BinaryUrl)) {
  $arch = Get-ClipServerWindowsArch
  $BinaryUrl = Join-ClipUrl -Root $BaseUrl -File "clip-server_windows_$arch.exe"
  Write-Host "Detected: windows/$arch"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$target = Join-Path $InstallDir "clip-server.exe"

Write-Host "Downloading $BinaryUrl"
Invoke-WebRequest -Uri $BinaryUrl -OutFile $target -UseBasicParsing

if (Get-Command Unblock-File -ErrorAction SilentlyContinue) {
  Unblock-File -Path $target -ErrorAction SilentlyContinue
}

if (-not $NoPathUpdate) {
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $pathItems = @()
  if (-not [string]::IsNullOrWhiteSpace($userPath)) {
    $pathItems = $userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
  }
  $alreadyOnPath = $pathItems | Where-Object { $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') }
  if (-not $alreadyOnPath) {
    $newUserPath = (@($pathItems) + $InstallDir) -join ';'
    [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
    $env:Path = (@($env:Path -split ';') + $InstallDir) -join ';'
    Write-Host "Added to user PATH: $InstallDir"
  }
}

Write-Host "Installed: $target"
Write-Host "Run: clip-server --host 0.0.0.0 --port 8787"