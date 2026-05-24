#!/bin/sh
set -eu

version="${1:-${CLIP_SERVER_VERSION:-0.1.0}}"
out_dir="${CLIP_SERVER_DIST_DIR:-dist}"
mkdir -p "$out_dir"

build_target() {
  goos="$1"
  goarch="$2"
  suffix="$3"
  output="$out_dir/clip-server_${goos}_${goarch}${suffix}"
  printf '%s\n' "building $output"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$output" .
}

build_target linux amd64 ""
build_target linux arm64 ""
build_target darwin amd64 ""
build_target darwin arm64 ""
build_target windows amd64 ".exe"
build_target windows arm64 ".exe"

cp site/index.html "$out_dir/index.html"
cp scripts/install.sh "$out_dir/install.sh"
cp scripts/install.ps1 "$out_dir/install.ps1"
chmod 755 "$out_dir/install.sh"

printf '%s\n' "release binaries written to $out_dir"
