#!/bin/sh
# Install the latest published dpad release for the current user.
set -eu

install_dpad() {
  case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) echo "Use this installer on macOS or Linux." >&2; return 1 ;;
  esac
  case $(uname -m) in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) echo "Unsupported CPU architecture: $(uname -m)" >&2; return 1 ;;
  esac

  asset=dpad-$os-$arch.tar.gz
  url=https://github.com/spar/dpad/releases/latest/download
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  cd "$tmp"
  curl -fsSL "$url/$asset" -o "$asset"
  curl -fsSL "$url/SHA256SUMS" -o SHA256SUMS
  awk -v asset="$asset" '$2 == asset { print; found=1 } END { exit !found }' SHA256SUMS > selected.sha256
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c selected.sha256
  else
    shasum -a 256 -c selected.sha256
  fi

  tar -xzf "$asset" dpad

  bin=$HOME/.local/bin
  mkdir -p "$bin"
  install -m 755 dpad "$bin/dpad"
  ln -sf dpad "$bin/dp"
  "$bin/dpad" --version
  case :$PATH: in
    *:"$bin":*) ;;
    *) echo 'Add ~/.local/bin to PATH in your shell profile to run dp by name.' ;;
  esac
}

install_dpad
