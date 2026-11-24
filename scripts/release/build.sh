#!/usr/bin/env bash
# Build release archives into a fresh directory using the same flags as make build.
# Archive names carry no version so releases/latest/download URLs stay stable.
set -euo pipefail
version=${1:?expected a release tag}
out=${2:?expected an output directory}
mkdir "$out"
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    name="dpad-${os}-${arch}"
    dir="$stage/$name"
    mkdir "$dir"
    cp LICENSE README.md "$dir/"
    if [[ $os == windows ]]; then
      GOOS="$os" GOARCH="$arch" make build VERSION="$version" BIN="$dir/dpad.exe"
      go run ./scripts/release/zip "$out/$name.zip" "$dir/dpad.exe" "$dir/LICENSE" "$dir/README.md"
    else
      GOOS="$os" GOARCH="$arch" make build VERSION="$version" BIN="$dir/dpad"
      tar -czf "$out/$name.tar.gz" -C "$dir" dpad LICENSE README.md
    fi
  done
done
(cd "$out" && sha256sum dpad-* > SHA256SUMS)
