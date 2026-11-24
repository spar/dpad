const { test } = require('node:test');
const assert = require('node:assert/strict');
const { parseSums, formula, scoop, pkgbuild, srcinfo } = require('./packages.cjs');

const names = ['darwin-amd64.tar.gz', 'darwin-arm64.tar.gz', 'linux-amd64.tar.gz',
  'linux-arm64.tar.gz', 'windows-amd64.zip', 'windows-arm64.zip'].map(n => 'dpad-' + n);
const hash = i => String(i).repeat(64);
const sums = parseSums(names.map((n, i) => `${hash(i)}  ${n}`).join('\n') + '\n');
const url = name => 'https://github.com/spar/dpad/releases/download/v1.2.3/' + name;

test('parses sha256sum output', () => {
  assert.equal(Object.keys(sums).length, 6);
  assert.equal(sums['dpad-windows-arm64.zip'], hash(5));
  assert.deepEqual(parseSums(`${hash(7)} *dpad-x.zip\nnot a sum\n`), { 'dpad-x.zip': hash(7) });
});

test('renders a Homebrew formula with a URL and checksum per platform', () => {
  const rb = formula('v1.2.3', sums);
  assert.match(rb, /^ {2}version "1\.2\.3"$/m);
  for (const [i, name] of names.slice(0, 4).entries()) {
    assert.ok(rb.includes(`url "${url(name)}"\n      sha256 "${hash(i)}"`), name);
  }
  assert.match(rb, /on_macos do\n {4}on_arm do\n {6}url "[^"]+darwin-arm64/);
  assert.match(rb, /bin\.install_symlink "dpad" => "dp"/);
});

test('renders a Scoop manifest for both Windows architectures', () => {
  const m = JSON.parse(scoop('v1.2.3', sums));
  assert.equal(m.version, '1.2.3');
  assert.deepEqual(m.architecture['64bit'], { url: url('dpad-windows-amd64.zip'), hash: hash(4) });
  assert.deepEqual(m.architecture.arm64, { url: url('dpad-windows-arm64.zip'), hash: hash(5) });
  assert.deepEqual(m.bin, ['dpad.exe', ['dpad.exe', 'dp']]);
});

test('renders matching PKGBUILD and .SRCINFO for the AUR', () => {
  const pkg = pkgbuild('v1.2.3', sums);
  const info = srcinfo('v1.2.3', sums);
  assert.match(pkg, /^pkgver=1\.2\.3$/m);
  assert.ok(pkg.includes(`source_x86_64=("dpad-1.2.3-x86_64.tar.gz::${url('dpad-linux-amd64.tar.gz')}")`));
  assert.ok(pkg.includes(`sha256sums_aarch64=('${hash(3)}')`));
  assert.ok(info.includes(`\tsource_aarch64 = dpad-1.2.3-aarch64.tar.gz::${url('dpad-linux-arm64.tar.gz')}`));
  assert.ok(info.includes(`\tsha256sums_x86_64 = ${hash(2)}`));
});

test('rejects missing assets and odd tags', () => {
  const { 'dpad-linux-arm64.tar.gz': _, ...partial } = sums;
  assert.throws(() => formula('v1.2.3', partial), /dpad-linux-arm64\.tar\.gz/);
  assert.throws(() => scoop('main', sums), /unexpected release tag/);
});
