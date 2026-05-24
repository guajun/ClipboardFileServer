param(
  [string]$BinaryUrl = $env:CLIP_SERVER_BINARY_URL,
  [string]$InstallDir = $(if ($env:CLIP_SERVER_INSTALL_DIR) { $env:CLIP_SERVER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\clip-server" }),
  [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($BinaryUrl)) {
  throw "BinaryUrl is required. Example: & ([scriptblock]::Create((Invoke-RestMethod 'https://your-server/clipboard/install.ps1'))) -BinaryUrl 'https://your-server/clipboard/clip-server_windows_amd64.exe'"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$target = Join-Path $InstallDir "clip-server.exe"

Write-Host "Downloading $BinaryUrl"
Invoke-WebRequest -Uri $BinaryUrl -OutFile $target

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