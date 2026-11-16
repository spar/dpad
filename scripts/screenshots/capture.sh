#!/usr/bin/env bash
# Regenerates the README screenshots and demo GIF from the running demo
# environment. See scripts/screenshots/README.md.
#
# usage: scripts/screenshots/capture.sh [OUT_DIR]   (default: docs/images)
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
out="${1:-$root/docs/images}"

for tool in python3 node npm ffmpeg docker; do
	command -v "$tool" >/dev/null || { echo "capture: $tool is required" >&2; exit 1; }
done
python3 -c 'import PIL' 2>/dev/null || { echo "capture: Pillow is required (pip install pillow)" >&2; exit 1; }
if [ -z "$(docker ps -q --filter label=dn-demo=true)" ]; then
	echo "capture: the demo environment isn't running; start it with make demo-up" >&2
	exit 1
fi
[ -d "$here/node_modules" ] || npm --prefix "$here" ci --no-audit --no-fund

make -C "$root" --no-print-directory build
dn="$root/bin/dockernext"
mkdir -p "$out"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/png" "$work/frames"

echo "capture: recording the TUI (about 100 seconds; restarts, pauses and stops a few demo containers)"
python3 "$here/drive.py" "$work/raw" 150 30 "$here/tui.json" -- "$dn"
echo "capture: recording CLI commands"
python3 "$here/drive.py" "$work/raw" 160 39 "$here/cli.json" -- bash "$here/cli.sh" "$dn"

echo "capture: rendering"
stills=()
for name in containers stats projects logs details events; do
	stills+=("$work/raw/$name.bin:$work/png/$name.png")
done
node "$here/render.js" 150 30 2 dn "${stills[@]}"
node "$here/render.js" 160 39 2 '~/app' "$work/raw/cli.bin:$work/png/cli.png"
frames=()
for f in "$work"/raw/tour-*.bin; do
	frames+=("$f:$work/frames/$(basename "$f" .bin).png")
done
node "$here/render.js" --seq --flat 150 30 1 dn "${frames[@]}"

python3 "$here/optimize.py" "$out" "$work"/png/*.png
ffmpeg -loglevel error -y -framerate 8 -i "$work/frames/tour-%04d.png" \
	-vf "split[a][b];[a]palettegen=max_colors=96:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" \
	-loop 0 "$out/demo.gif"
echo "capture: wrote $out"
ls -l "$out"
