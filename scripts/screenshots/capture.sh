#!/usr/bin/env bash
# Regenerates the README screenshots and demo GIF from the running demo
# environment. See scripts/screenshots/README.md.
#
# usage: scripts/screenshots/capture.sh [OUT_DIR]   (default: docs/images)
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
out="${1:-$root/docs/images}"

for tool in python3 node npm docker; do
	command -v "$tool" >/dev/null || { echo "capture: $tool is required" >&2; exit 1; }
done
python3 -c 'import fontTools, brotli' 2>/dev/null || { echo "capture: fonttools is required (pip install 'fonttools[woff]')" >&2; exit 1; }
if [ -z "$(docker ps -q --filter label=dp-demo=true)" ]; then
	echo "capture: the demo environment isn't running; start it with make demo-up" >&2
	exit 1
fi
[ -d "$here/node_modules" ] || npm --prefix "$here" ci --no-audit --no-fund

make -C "$root" --no-print-directory build
dp="$root/bin/dpad"
mkdir -p "$out"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "capture: recording the TUI (about 100 seconds; restarts, pauses and stops a few demo containers)"
# The stand-in editor gives the Compose edit review a diff; the scenario discards it.
VISUAL="$here/editor.sh" python3 "$here/drive.py" "$work/raw" 150 30 "$here/tui.json" -- "$dp"
echo "capture: recording the theme gallery"
# No user config, so the picker lists only the built-in themes.
DPAD_CONFIG="$work/none.yaml" DPAD_THEME=default python3 "$here/drive.py" "$work/raw" 150 30 "$here/themes.json" -- "$dp"
echo "capture: recording the settings screen"
# A fresh HOME, so settings shows the default config path and a plain $EDITOR;
# DOCKER_CONFIG keeps your Docker contexts.
mkdir -p "$work/home"
env -u DPAD_CONFIG -u XDG_CONFIG_HOME DOCKER_CONFIG="${DOCKER_CONFIG:-$HOME/.docker}" HOME="$work/home" EDITOR=vim VISUAL= DPAD_THEME=default python3 "$here/drive.py" "$work/raw" 120 13 "$here/settings.json" -- "$dp"
echo "capture: recording CLI commands"
python3 "$here/drive.py" "$work/raw" 160 39 "$here/cli.json" -- bash "$here/cli.sh" "$dp"

echo "capture: rendering"
for name in containers stats projects compose-edit logs details events prune; do
	node "$here/svg.js" 150 30 dp "$out/$name.svg" "$work/raw/$name.bin"
done
node "$here/svg.js" 120 13 dp "$out/settings.svg" "$work/raw/settings.bin"
mkdir -p "$out/themes"
for snap in "$work"/raw/theme-*.bin; do
	name="$(basename "$snap" .bin)"
	node "$here/svg.js" 150 30 dp "$out/themes/${name#theme-}.svg" "$snap"
done
node "$here/svg.js" 160 39 '~/app' "$out/cli.svg" "$work/raw/cli.bin"
node "$here/svg.js" --anim 10 150 30 dp "$out/demo.svg" "$work"/raw/tour-*.bin
python3 "$here/font.py" "$out"/*.svg "$out"/themes/*.svg
echo "capture: wrote $out"
