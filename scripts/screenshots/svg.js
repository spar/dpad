// Replays output saved by drive.py in a headless xterm.js terminal and writes
// it as SVG: text stays text, so the image is sharp at any size and zoom.
//
// usage: node svg.js COLS ROWS TITLE OUT.svg IN.bin
//        node svg.js --anim FPS COLS ROWS TITLE OUT.svg IN-0000.bin IN-0001.bin...
//
// With --anim the inputs are successive recordings of one session, taken FPS
// times a second; they become one looping CSS animation. Each distinct row is
// drawn once and reused, so frames cost little. font.py embeds the font later.
const { Terminal } = require('@xterm/headless');
const fs = require('fs');

const args = process.argv.slice(2);
let fps = 0;
if (args[0] === '--anim') {
  args.shift();
  fps = Number(args.shift());
}
const [cols, rows] = args.slice(0, 2).map(Number);
const [title, output, ...inputs] = args.slice(2);

const theme = {
  background: '#141721', foreground: '#d8dee9',
  palette: [
    '#1d2130', '#f2777a', '#8fd18b', '#f6c177', '#7aa2f7', '#c49cf2', '#6fd3e3', '#c7ccd8',
    '#5c6377', '#ff8f92', '#a6e3a1', '#f9d58e', '#94b6ff', '#d6b4fa', '#8be4f0', '#eef1f6',
  ],
};

// Cell size in px for a 14px font; JetBrains Mono advances 0.6em.
const FS = 14;
const CW = 8.4;
const CH = 17;
const BASE = 13; // baseline offset within a cell
const PAD = { x: 16, top: 14, bottom: 16 };
const BAR = 38;
const MARGIN = 28;
const screenW = cols * CW;
const screenH = rows * CH;
const winW = screenW + 2 * PAD.x;
const winH = BAR + PAD.top + screenH + PAD.bottom;

const n = (v) => +v.toFixed(2);
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

function xterm256(i) {
  if (i < 16) return theme.palette[i];
  if (i >= 232) {
    const v = 8 + (i - 232) * 10;
    return '#' + v.toString(16).padStart(2, '0').repeat(3);
  }
  i -= 16;
  const c = [Math.floor(i / 36), Math.floor(i / 6) % 6, i % 6].map((v) => (v ? 55 + v * 40 : 0));
  return '#' + c.map((v) => v.toString(16).padStart(2, '0')).join('');
}

const rgb = (v) => '#' + v.toString(16).padStart(6, '0');

// A color is either a palette class ("p4") or a literal fill.
function color(cell, fgSide) {
  if (fgSide ? cell.isFgDefault() : cell.isBgDefault()) return fgSide ? 'fg' : 'bg';
  const v = fgSide ? cell.getFgColor() : cell.getBgColor();
  if (fgSide ? cell.isFgRGB() : cell.isBgRGB()) return rgb(v);
  return v < 16 ? 'p' + v : xterm256(v);
}

const fill = (c) => (c.startsWith('#') ? `fill="${c}"` : `class="${c}"`);

// Block elements are drawn as rectangles so bars and sparklines join up
// without the gaps a font's glyphs leave between rows.
// Values: [x, y, w, h] as fractions of a cell, and an optional opacity.
const BLOCKS = {
  '█': [0, 0, 1, 1], '▀': [0, 0, 1, 0.5], '▔': [0, 0, 1, 0.125],
  '▌': [0, 0, 0.5, 1], '▐': [0.5, 0, 0.5, 1], '▕': [0.875, 0, 0.125, 1],
  '░': [0, 0, 1, 1, 0.25], '▒': [0, 0, 1, 1, 0.5], '▓': [0, 0, 1, 1, 0.75],
};
'▁▂▃▄▅▆▇'.split('').forEach((ch, i) => (BLOCKS[ch] = [0, 1 - (i + 1) / 8, 1, (i + 1) / 8]));
'▉▊▋▌▍▎▏'.split('').forEach((ch, i) => (BLOCKS[ch] = [0, 0, (7 - i) / 8, 1]));

// Box drawing as lines: which arms reach the left, right, top and bottom edge.
const BOX = {
  '─': 'lr', '━': 'lr', '│': 'tb', '┃': 'tb', '┌': 'rb', '┐': 'lb', '└': 'rt', '┘': 'lt',
  '├': 'tbr', '┤': 'tbl', '┬': 'lrb', '┴': 'lrt', '┼': 'lrtb',
  '╭': 'rb~', '╮': 'lb~', '╰': 'rt~', '╯': 'lt~',
};

// Not in JetBrains Mono, so drawn rather than left to the viewer's fonts.
const HEART = 'M.5 .95C.1 .65 0 .45 0 .3 0 .12 .14 0 .28 0 .38 0 .46 .06 .5 .15 .54 .06 .62 0 .72 0 .86 0 1 .12 1 .3 1 .45 .9 .65 .5 .95z';

// Braille is missing from JetBrains Mono too; each raised dot becomes a circle.
const BRAILLE_DOTS = [[0, 0], [0, 1], [0, 2], [1, 0], [1, 1], [1, 2], [0, 3], [1, 3]];

function brailleDots(ch, x, c, dim) {
  const bits = ch.codePointAt(0) - 0x2800;
  const op = dim ? ' fill-opacity=".5"' : '';
  return BRAILLE_DOTS.filter((_, i) => bits & (1 << i)).map(([col, row]) =>
    `<circle cx="${n(x + CW * (0.3 + 0.4 * col))}" cy="${n(CH * (0.14 + 0.24 * row))}" r="1.2" ${fill(c)}${op}/>`).join('');
}

function boxPath(ch, x) {
  const arms = BOX[ch];
  const cx = n(x + CW / 2);
  const cy = CH / 2;
  const end = { l: [n(x), cy], r: [n(x + CW), cy], t: [cx, 0], b: [cx, CH] };
  if (arms.endsWith('~')) {
    const [a, b] = [end[arms[0]], end[arms[1]]];
    return `M${a[0]} ${a[1]}Q${cx} ${cy} ${b[0]} ${b[1]}`;
  }
  return arms.split('').map((k) => `M${cx} ${cy}L${end[k][0]} ${end[k][1]}`).join('');
}

// Draws one terminal row at y=0 as an SVG fragment.
function drawRow(line, cell) {
  const bg = [];
  const shapes = [];
  const lines = {};
  const texts = [];
  let run = null;
  const flush = () => {
    if (run && run.text.trim()) {
      const cls = [run.fg.startsWith('#') ? '' : run.fg, run.bold ? 'b' : '', run.dim ? 'd' : '', run.italic ? 'i' : '', run.underline ? 'u' : '']
        .filter(Boolean).join(' ');
      const attrs = (cls ? ` class="${cls}"` : '') + (run.fg.startsWith('#') ? ` style="fill:${run.fg}"` : '');
      texts.push(`<text x="${n(run.x * CW)}" y="${BASE}"${attrs}>${esc(run.text.replace(/\s+$/, ''))}</text>`);
    }
    run = null;
  };
  let bgRun = null;
  const flushBg = () => {
    if (bgRun) bg.push(`<rect x="${n(bgRun.x * CW)}" width="${n(bgRun.w * CW)}" height="${CH}" ${fill(bgRun.c)}/>`);
    bgRun = null;
  };
  let blockRun = null;
  const flushBlock = () => {
    if (blockRun) {
      const [bx, by, bw, bh, op] = BLOCKS[blockRun.ch];
      const w = blockRun.w - 1 + bw;
      shapes.push(`<rect x="${n((blockRun.x + bx) * CW)}" y="${n(by * CH)}" width="${n(w * CW)}" height="${n(bh * CH)}" ${fill(blockRun.c)}${op ? ` fill-opacity="${op}"` : ''}/>`);
    }
    blockRun = null;
  };

  for (let x = 0; x < cols; x++) {
    line.getCell(x, cell);
    const width = cell.getWidth();
    if (width === 0) continue;
    let fg = color(cell, true);
    let bgc = color(cell, false);
    if (cell.isInverse()) [fg, bgc] = [bgc, fg];
    if (bgc !== 'bg') {
      if (bgRun && bgRun.c === bgc && bgRun.x + bgRun.w === x) bgRun.w += width;
      else (flushBg(), (bgRun = { x, w: width, c: bgc }));
    } else flushBg();

    const ch = cell.getChars() || ' ';
    const invisible = cell.isInvisible();
    const dimOp = cell.isDim() ? 0.5 : 1;
    if (!invisible && BLOCKS[ch] && dimOp === 1) {
      flush();
      if (blockRun && blockRun.ch === ch && blockRun.c === fg && blockRun.x + blockRun.w === x && BLOCKS[ch][2] === 1) blockRun.w++;
      else (flushBlock(), (blockRun = { ch, c: fg, x, w: 1 }));
      continue;
    }
    flushBlock();
    if (!invisible && ch === '♥') {
      flush();
      shapes.push(`<path d="${HEART}" transform="translate(${n(x * CW + 0.6)} 5.6) scale(7.2 6.8)" ${fill(fg)}/>`);
      continue;
    }
    if (!invisible && ch >= '\u2800' && ch <= '\u28ff') {
      flush();
      shapes.push(brailleDots(ch, x * CW, fg, cell.isDim()));
      continue;
    }
    if (!invisible && BOX[ch]) {
      flush();
      (lines[fg] = lines[fg] || []).push(boxPath(ch, x * CW));
      continue;
    }
    const style = { fg, bold: !!cell.isBold(), dim: !!cell.isDim(), italic: !!cell.isItalic(), underline: !!cell.isUnderline() };
    const text = invisible ? ' ' : ch;
    const same = run && width === 1 && run.wide === false &&
      ['fg', 'bold', 'dim', 'italic', 'underline'].every((k) => run[k] === style[k]);
    if (same) run.text += text;
    else if (!run && text === ' ') continue;
    else (flush(), (run = { x, text, wide: width !== 1, ...style }));
    if (width !== 1) flush();
  }
  flush();
  flushBg();
  flushBlock();
  const strokes = Object.entries(lines).map(([c, d]) =>
    `<path d="${d.join('')}" ${c.startsWith('#') ? `stroke="${c}"` : `class="s${c}"`}/>`);
  return bg.join('') + shapes.join('') + strokes.join('') + texts.join('');
}

function snapshot(term, cell) {
  const buf = term.buffer.active;
  const out = [];
  for (let y = 0; y < rows; y++) out.push(drawRow(buf.getLine(buf.viewportY + y), cell));
  return out;
}

// A theme can set the terminal's default colors (OSC 10 and 11) while dp
// runs; draw the screen in the last ones set, as a terminal would.
const initial = { foreground: theme.foreground, background: theme.background };
function defaultColors(text) {
  const re = /\x1b\](1[01]);(#[0-9a-fA-F]{6})(?:\x07|\x1b\\)|\x1b\](11[01])(?:\x07|\x1b\\)/g;
  for (const [, set, hex, reset] of text.matchAll(re)) {
    if (set) theme[set === '10' ? 'foreground' : 'background'] = hex;
    else if (reset === '110') theme.foreground = initial.foreground;
    else theme.background = initial.background;
  }
}

const write = (term, data) => new Promise((resolve) => term.write(data, resolve));

(async () => {
  const term = new Terminal({ cols, rows, scrollback: 0, allowProposedApi: true });
  const cell = term.buffer.active.getNullCell();
  const frames = [];
  let written = 0;
  for (const input of inputs) {
    const all = fs.readFileSync(input);
    await write(term, all.subarray(fps ? written : 0));
    written = all.length;
    frames.push(snapshot(term, cell));
    if (!fps) defaultColors(all.toString('latin1'));
  }

  const rowIds = new Map();
  const defs = [];
  const useRows = (frame) => frame.map((svg, y) => {
    if (!svg) return '';
    if (!fps) return `<g transform="translate(0 ${y * CH})">${svg}</g>`;
    if (!rowIds.has(svg)) {
      rowIds.set(svg, 'r' + rowIds.size.toString(36));
      defs.push(`<g id="${rowIds.get(svg)}">${svg}</g>`);
    }
    return `<use href="#${rowIds.get(svg)}" y="${y * CH}"/>`;
  }).join('');

  let screen;
  let css = '';
  if (fps) {
    // Merge repeated frames, then hold the last one before looping.
    const shown = [];
    for (const f of frames) {
      const key = f.join('\n');
      if (shown.length && shown[shown.length - 1].key === key) shown[shown.length - 1].n++;
      else shown.push({ key, f, n: 1 });
    }
    shown[shown.length - 1].n += 2 * fps;
    const total = shown.reduce((s, f) => s + f.n, 0);
    let t = 0;
    const keys = [];
    const body = shown.map((f, i) => {
      keys.push(`${n((t / total) * 100)}%{transform:translateY(${-i * screenH}px)}`);
      t += f.n;
      return `<g transform="translate(0 ${i * screenH})">${useRows(f.f)}</g>`;
    });
    keys.push(`100%{transform:translateY(${-(shown.length - 1) * screenH}px)}`);
    css = `#strip{animation:play ${n(total / fps)}s steps(1,end) infinite}@keyframes play{${keys.join('')}}`;
    screen = `<g id="strip">${body.join('')}</g>`;
  } else {
    screen = useRows(frames[frames.length - 1]);
  }

  const pal = theme.palette.map((c, i) => `.p${i}{fill:${c}}.sp${i}{stroke:${c}}`).join('');
  const W = n(winW + 2 * MARGIN);
  const H = n(winH + 2 * MARGIN);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" xml:space="preserve">
<style>/*fonts*/
text{font-family:'dp mono',ui-monospace,Menlo,Consolas,monospace;font-size:${FS}px;white-space:pre;fill:${theme.foreground}}
.fg{fill:${theme.foreground}}.bg{fill:${theme.background}}.sfg{stroke:${theme.foreground}}.sbg{stroke:${theme.background}}
path{fill:none;stroke-width:1.2}.b{font-weight:700}.d{opacity:.5}.i{font-style:italic}.u{text-decoration:underline}
${pal}${css}</style>
<defs><filter id="shadow" x="-10%" y="-10%" width="120%" height="130%"><feDropShadow dx="0" dy="14" stdDeviation="14" flood-opacity=".45"/></filter>
<clipPath id="clip"><rect width="${n(screenW)}" height="${screenH}"/></clipPath>${defs.join('\n')}</defs>
<g transform="translate(${MARGIN} ${MARGIN})">
<rect width="${n(winW)}" height="${winH}" rx="12" fill="${theme.background}" filter="url(#shadow)"/>
<path d="M0 ${BAR}V12a12 12 0 0 1 12-12H${n(winW - 12)}a12 12 0 0 1 12 12V${BAR}z" style="fill:#1b1f2b;stroke:none"/>
<rect y="${BAR}" width="${n(winW)}" height="1" fill="#232838"/>
<circle cx="20" cy="19" r="6" fill="#ff5f57"/><circle cx="40" cy="19" r="6" fill="#febc2e"/><circle cx="60" cy="19" r="6" fill="#28c840"/>
<text x="${n(winW / 2)}" y="23.5" text-anchor="middle" style="font-size:13px;font-weight:500;fill:#8a91a5">${esc(title)}</text>
<rect width="${n(winW)}" height="${winH}" rx="12" style="fill:none;stroke:#ffffff;stroke-opacity:.07"/>
<g transform="translate(${PAD.x} ${BAR + PAD.top})" clip-path="url(#clip)">${screen}</g>
</g></svg>
`;
  fs.writeFileSync(output, svg);
  console.log(`${output}: ${frames.length} frame(s), ${(svg.length / 1024).toFixed(0)} KiB before fonts`);
})();
