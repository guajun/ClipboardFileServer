#!/bin/sh
set -eu

binary_url="${1:-${CLIP_SERVER_BINARY_URL:-}}"
install_dir="${2:-${CLIP_SERVER_INSTALL_DIR:-}}"

if [ -z "$binary_url" ]; then
  printf '%s\n' "Binary URL is required." >&2
  printf '%s\n' "Example:" >&2
  printf '%s\n' "  curl -fsSL https://your-server/clipboard/install.sh | sh -s -- https://your-server/clipboard/clip-server_linux_amd64" >&2
  exit 2
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

mkdir -p "$install_dir"
temp_file="${TMPDIR:-/tmp}/clip-server.$$"
trap 'rm -f "$temp_file"' EXIT INT TERM

printf '%s\n' "Downloading $binary_url"
download_file "$binary_url" "$temp_file"
chmod 755 "$temp_file"
mv "$temp_file" "$install_dir/clip-server"

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