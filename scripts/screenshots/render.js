// Replays output saved by drive.py in xterm.js inside headless Chromium and
// screenshots it in a window frame.
//
// usage: node render.js [--seq] [--flat] COLS ROWS SCALE TITLE IN.bin:OUT.png...
//   --seq   inputs are successive recordings of one session: write only the new
//           bytes of each into the same terminal (fast for animation frames)
//   --flat  square corners and no shadow or margin, for GIF frames
const { chromium } = require('playwright');
const fs = require('fs');
const os = require('os');
const path = require('path');

const args = process.argv.slice(2);
const flags = new Set();
while (args[0]?.startsWith('--')) flags.add(args.shift());
const seq = flags.has('--seq');
const flat = flags.has('--flat');
const [cols, rows, scale, title, ...pairs] = args;
const xtermDir = path.join(__dirname, 'node_modules/@xterm/xterm');

const theme = {
  background: '#141721', foreground: '#d8dee9', cursor: '#141721', cursorAccent: '#141721',
  selectionBackground: '#2e3446',
  black: '#1d2130', red: '#f2777a', green: '#8fd18b', yellow: '#f6c177',
  blue: '#7aa2f7', magenta: '#c49cf2', cyan: '#6fd3e3', white: '#c7ccd8',
  brightBlack: '#5c6377', brightRed: '#ff8f92', brightGreen: '#a6e3a1', brightYellow: '#f9d58e',
  brightBlue: '#94b6ff', brightMagenta: '#d6b4fa', brightCyan: '#8be4f0', brightWhite: '#eef1f6',
};

const html = `<!doctype html><html><head><meta charset="utf-8">
<link rel="stylesheet" href="file://${xtermDir}/css/xterm.css">
<style>
  html,body{margin:0;background:transparent}
  #wrap{display:inline-block;padding:${flat ? 0 : 28}px}
  #win{background:${theme.background};overflow:hidden;
       ${flat ? '' : 'border-radius:12px;box-shadow:0 18px 50px rgba(0,0,0,.45),0 0 0 1px rgba(255,255,255,.07);'}}
  #bar{height:38px;display:flex;align-items:center;padding:0 14px;background:#1b1f2b;
       border-bottom:1px solid #232838;position:relative}
  .dot{width:12px;height:12px;border-radius:50%;margin-right:8px}
  #title{position:absolute;left:0;right:0;text-align:center;color:#8a91a5;
         font:500 13px/38px 'JetBrains Mono',monospace;pointer-events:none}
  #screen{padding:14px 16px 16px}
  .xterm-viewport{overflow:hidden!important}
</style></head><body>
<div id="wrap"><div id="win"><div id="bar">
<span class="dot" style="background:#ff5f57"></span><span class="dot" style="background:#febc2e"></span><span class="dot" style="background:#28c840"></span>
<div id="title"></div></div><div id="screen"></div></div></div>
<script src="file://${xtermDir}/lib/xterm.js"></script>
</body></html>`;

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ deviceScaleFactor: Number(scale), viewport: { width: 2400, height: 1600 } });
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'dp-render-'));
  fs.writeFileSync(path.join(dir, 'page.html'), html);
  await page.goto('file://' + path.join(dir, 'page.html'));
  // An installed font changes the text width from the fallback's.
  const hasFont = await page.evaluate(() => {
    const ctx = document.createElement('canvas').getContext('2d');
    const width = (font) => ((ctx.font = font), ctx.measureText('mmmmiiiiWWWW').width);
    return width("14px 'JetBrains Mono', serif") !== width('14px serif');
  });
  if (!hasFont) {
    console.warn('warning: JetBrains Mono is not installed; using the default monospace font');
  }
  let written = 0;
  for (const pair of pairs) {
    const i = pair.lastIndexOf(':');
    const [input, output] = [pair.slice(0, i), pair.slice(i + 1)];
    let bytes = fs.readFileSync(input);
    if (seq) {
      const all = bytes;
      bytes = all.subarray(written);
      written = all.length;
    }
    await page.evaluate(async ({ data, cols, rows, theme, title, seq }) => {
      document.getElementById('title').textContent = title;
      if (!seq || !window.xt) {
        const el = document.getElementById('screen');
        el.innerHTML = '';
        window.xt = new Terminal({
          cols, rows, theme, fontFamily: "'JetBrains Mono', monospace", fontSize: 14, lineHeight: 1.15,
          drawBoldTextInBrightColors: false, minimumContrastRatio: 1, scrollback: 0,
        });
        window.xt.open(el);
      }
      const bytes = Uint8Array.from(atob(data), (c) => c.charCodeAt(0));
      await new Promise((resolve) => window.xt.write(bytes, resolve));
      await new Promise((resolve) => setTimeout(resolve, 100));
    }, { data: bytes.toString('base64'), cols: Number(cols), rows: Number(rows), theme, title, seq });
    await page.locator('#wrap').screenshot({ path: output, omitBackground: true });
  }
  await browser.close();
  fs.rmSync(dir, { recursive: true });
})();
