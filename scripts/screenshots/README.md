# Screenshots

`capture.sh` regenerates the images in [docs/images](../../docs/images) from
real `dp` output, so the README stays accurate when the UI changes.

```bash
make demo-up
make screenshots
```

To write somewhere other than `docs/images`, run
`scripts/screenshots/capture.sh OUT_DIR`. A run takes about three minutes.

## Requirements

- Docker with the demo environment running (`make demo-up`)
- Go, to build `dp`
- Python 3 with [fonttools](https://pypi.org/project/fonttools/):
  `pip install 'fonttools[woff]'`
- Node.js and npm. The first run installs the pinned headless xterm.js into
  `node_modules` here.
- The [JetBrains Mono](https://www.jetbrains.com/lp/mono/) Regular and Bold
  TTF files. `font.py` looks in the usual font folders; set `DP_FONT_DIR` if
  they are elsewhere.

## How it works

1. `drive.py` runs `dp` in a pseudo-terminal at a fixed size, types the keys
   listed in a scenario (`tui.json`, `cli.json`) and saves everything `dp`
   writes at each `snap` step and, while recording, ten times a second.
   The TUI scenario restarts, pauses and stops a few demo containers so the
   Events tab has something to show; they are running again afterwards.
2. `svg.js` replays each saved stream in a headless
   [xterm.js](https://xtermjs.org/) terminal and writes the screen as SVG in
   a window frame. Text stays text and bars are drawn as shapes, so the
   images are sharp at any size or zoom. The recorded frames become
   `demo.svg`, a looping CSS animation that draws each distinct row once.
3. `font.py` embeds the JetBrains Mono glyphs each image uses, so the images
   look the same in every browser without the font installed.

The TUI scenario waits about a minute on the Stats tab first so the history
sparklines have data. To change what is captured, edit the scenario files;
the step types are described at the top of `drive.py`.
