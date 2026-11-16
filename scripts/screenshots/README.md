# Screenshots

`capture.sh` regenerates the images in [docs/images](../../docs/images) from
real `dn` output, so the README stays accurate when the UI changes.

```bash
make demo-up
make screenshots
```

To write somewhere other than `docs/images`, run
`scripts/screenshots/capture.sh OUT_DIR`. A run takes about three minutes.

## Requirements

- Docker with the demo environment running (`make demo-up`)
- Go, to build `dn`
- Python 3 with [Pillow](https://pypi.org/project/pillow/)
- Node.js and npm. The first run installs the pinned Playwright and xterm.js
  versions into `node_modules` here. If Playwright has no browser yet, run
  `npx --prefix scripts/screenshots playwright install chromium`.
- ffmpeg
- The [JetBrains Mono](https://www.jetbrains.com/lp/mono/) font. Without it
  the images fall back to the default monospace font.

## How it works

1. `drive.py` runs `dn` in a pseudo-terminal at a fixed size, types the keys
   listed in a scenario (`tui.json`, `cli.json`) and saves everything `dn`
   writes at each `snap` step and, while recording, eight times a second.
   The TUI scenario restarts, pauses and stops a few demo containers so the
   Events tab has something to show; they are running again afterwards.
2. `render.js` replays each saved stream in [xterm.js](https://xtermjs.org/)
   inside headless Chromium and screenshots it in a window frame. Stills are
   rendered at 2x for sharp text; GIF frames at 1x.
3. `optimize.py` reduces the stills to 256-color PNGs, and ffmpeg encodes the
   recorded frames as `demo.gif`.

The TUI scenario waits about a minute on the Stats tab first so the history
sparklines have data. To change what is captured, edit the scenario files;
the step types are described at the top of `drive.py`.
