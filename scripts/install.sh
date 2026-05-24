#!/bin/sh
set -eu

default_base_url="https://guajun.github.io/ClipboardFileServer"
first_arg="${1:-}"
second_arg="${2:-}"
binary_url="${CLIP_SERVER_BINARY_URL:-}"
base_url="${CLIP_SERVER_BASE_URL:-$default_base_url}"
install_dir="${CLIP_SERVER_INSTALL_DIR:-}"

if [ -n "$first_arg" ]; then
  case "$first_arg" in
    *clip-server_*)
      binary_url="$first_arg"
      ;;
    http://*|https://*)
      base_url="$first_arg"
      ;;
    *)
      install_dir="$first_arg"
      ;;
  esac
fi

if [ -n "$second_arg" ]; then
  install_dir="$second_arg"
fi

if [ -z "$install_dir" ]; then
  if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ] && [ -w "$PREFIX/bin" ]; then
    install_dir="$PREFIX/bin"
  else
    install_dir="$HOME/.local/bin"
  fi
fi

download_file() {
  url="$1"
  output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url"
    return
  fi
  printf '%s\n' "curl or wget is required to download $url" >&2
  exit 1
}

trim_trailing_slash() {
  value="$1"
  while [ "${value%/}" != "$value" ]; do
    value="${value%/}"
  done
  printf '%s' "$value"
}

detect_os() {
  os_name="$(uname -s 2>/dev/null || printf unknown)"
  case "$(printf '%s' "$os_name" | tr '[:upper:]' '[:lower:]')" in
    linux*)
      printf '%s' "linux"
      ;;
    darwin*)
      printf '%s' "darwin"
      ;;
    *)
      printf '%s\n' "Unsupported OS for install.sh: $os_name" >&2
      printf '%s\n' "Use install.ps1 on Windows. Supported Unix targets: linux/darwin amd64/arm64." >&2
      exit 2
      ;;
  esac
}

detect_arch() {
  arch_name="$(uname -m 2>/dev/null || printf unknown)"
  case "$(printf '%s' "$arch_name" | tr '[:upper:]' '[:lower:]')" in
    x86_64|amd64)
      printf '%s' "amd64"
      ;;
    aarch64|arm64)
      printf '%s' "arm64"
      ;;
    *)
      printf '%s\n' "Unsupported architecture: $arch_name" >&2
      printf '%s\n' "Supported architectures: amd64, arm64." >&2
      exit 2
      ;;
  esac
}

if [ -z "$binary_url" ]; then
  goos="$(detect_os)"
  goarch="$(detect_arch)"
  binary_url="$(trim_trailing_slash "$base_url")/clip-server_${goos}_${goarch}"
  printf '%s\n' "Detected: $goos/$goarch"
fi

mkdir -p "$install_dir"
temp_file="${TMPDIR:-/tmp}/clip-server.$$"
trap 'rm -f "$temp_file"' EXIT INT TERM

printf '%s\n' "Downloading $binary_url"
download_file "$binary_url" "$temp_file"
chmod 755 "$temp_file"
mv -f "$temp_file" "$install_dir/clip-server"

printf '%s\n' "Installed: $install_dir/clip-server"
case ":$PATH:" in
  *":$install_dir:"*)
    printf '%s\n' "Run: clip-server --host 0.0.0.0 --port 8787"
    ;;
  *)
    printf '%s\n' "Add this directory to PATH: $install_dir" >&2
    printf '%s\n' "Or run: $install_dir/clip-server --host 0.0.0.0 --port 8787"
    ;;
esac