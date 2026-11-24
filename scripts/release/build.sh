#!/usr/bin/env bash
# Build release assets into a fresh directory using the same flags as make build.
set -euo pipefail
version=${1:?expected a release tag}
out=${2:?expected an output directory}
mkdir "$out"
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    name="dpad-${os}-${arch}"
    if [[ $os == windows ]]; then name+=.exe; fi
    GOOS="$os" GOARCH="$arch" make build VERSION="$version" BIN="$out/$name"
  done
done
(cd "$out" && sha256sum dpad-* > SHA256SUMS)
