param(
  [string]$Version = $(if ($env:CLIP_SERVER_VERSION) { $env:CLIP_SERVER_VERSION } else { "0.1.0" }),
  [string]$OutDir = $(if ($env:CLIP_SERVER_DIST_DIR) { $env:CLIP_SERVER_DIST_DIR } else { "dist" })
)

$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$targets = @(
  @{ GOOS = "linux"; GOARCH = "amd64"; Suffix = "" },
  @{ GOOS = "linux"; GOARCH = "arm64"; Suffix = "" },
  @{ GOOS = "darwin"; GOARCH = "amd64"; Suffix = "" },
  @{ GOOS = "darwin"; GOARCH = "arm64"; Suffix = "" },
  @{ GOOS = "windows"; GOARCH = "amd64"; Suffix = ".exe" },
  @{ GOOS = "windows"; GOARCH = "arm64"; Suffix = ".exe" }
)

foreach ($target in $targets) {
  $env:CGO_ENABLED = "0"
  $env:GOOS = $target.GOOS
  $env:GOARCH = $target.GOARCH
  $output = Join-Path $OutDir "clip-server_$($target.GOOS)_$($target.GOARCH)$($target.Suffix)"
  Write-Host "building $output"
  & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $output .
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed for $($target.GOOS)/$($target.GOARCH)"
  }
}

Copy-Item -Path "site\index.html" -Destination (Join-Path $OutDir "index.html") -Force
Copy-Item -Path "scripts\install.sh" -Destination (Join-Path $OutDir "install.sh") -Force
Copy-Item -Path "scripts\install.ps1" -Destination (Join-Path $OutDir "install.ps1") -Force

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
Write-Host "release binaries written to $OutDir"
