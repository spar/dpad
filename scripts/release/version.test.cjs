const { test } = require('node:test');
const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const { mkdtempSync, rmSync } = require('node:fs');
const { tmpdir } = require('node:os');
const path = require('node:path');
const { bumpFor } = require('./version.cjs');

test('classifies conventional commits', () => {
  for (const [subject, bump] of [
    ['feat(ui): add a panel', 2], ['fix: close stream', 1], ['perf(stats): reuse samples', 1],
    ['docs: example', 0], ['chore: tidy', 0], ['test: add fixture', 0], ['ci: update', 0],
    ['refactor(core)!: change API', 3], ['fix: change output\n\nBREAKING CHANGE: renamed field', 3],
    ['feat: change API\n\nBREAKING-CHANGE: removed flag', 3], ['Merge pull request #42', 0],
  ]) assert.equal(bumpFor(subject), bump, subject);
});

test('plans releases against tagged Git history, including reruns and older checkouts', t => {
  const cwd = mkdtempSync(path.join(tmpdir(), 'dpad-release-test-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', ['-c', 'user.name=Test', '-c', 'user.email=test@example.org', ...args], { cwd });
  const commit = message => git('commit', '--allow-empty', '-m', message);
  const tag = () => execFileSync(process.execPath, [require.resolve('./version.cjs')], { cwd, encoding: 'utf8' }).trim();
  git('init', '-q');
  commit('feat!: initial history');
  assert.equal(tag(), 'v0.1.0');
  git('tag', 'v0.1.0');
  assert.equal(tag(), '');
  commit('docs: examples');
  assert.equal(tag(), '');
  commit('fix: bounds');
  assert.equal(tag(), 'v0.1.1');
  commit('feat(ui): filters');
  assert.equal(tag(), 'v0.2.0');
  commit('feat!: change flags');
  assert.equal(tag(), 'v1.0.0');
  git('tag', 'v1.0.0');
  git('checkout', '-q', 'HEAD~1');
  assert.equal(tag(), '');
});
