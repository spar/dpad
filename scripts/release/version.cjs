// Print the next tag, or nothing when this checkout has nothing to release.
const { execFileSync, spawnSync } = require('node:child_process');

function bumpFor(message) {
  const [subject, ...body] = message.split('\n');
  const header = /^(\w+)(?:\([^)]+\))?(!)?: .+/.exec(subject);
  if (!header) return 0;
  if (header[2] || body.some(line => /^BREAKING[ -]CHANGE: \S/.test(line))) return 3;
  if (header[1] === 'feat') return 2;
  return ['fix', 'perf'].includes(header[1]) ? 1 : 0;
}

// Only files that end up in the release binaries or archives can cut a release,
// so a feat: or fix: that changes just docs, the website, CI or tests doesn't.
// A new Go package directory outside these needs adding here.
function shipsInRelease(file) {
  if (file === 'go.mod' || file === 'go.sum' || file === 'main.go' || file === 'scripts/release/build.sh') return true;
  if (!/^(cmd|internal|themes|scripts\/release\/zip)\//.test(file)) return false;
  return !file.endsWith('_test.go') && !file.split('/').includes('testdata');
}

function nextTag() {
  const git = (...args) => execFileSync('git', args, { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
  const previous = git('tag', '--list', 'v*', '--sort=-version:refname').split('\n')
    .find(tag => /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(tag));
  if (!previous) return 'v0.1.0';

  // An older CI run can finish after a newer one has published.
  const ancestry = spawnSync('git', ['merge-base', '--is-ancestor', previous, 'HEAD']);
  if (ancestry.status === 1) return '';
  if (ancestry.status !== 0) throw new Error('Could not check release tag ancestry');

  // Each record is a commit's message, then the files it changed. Merge commits
  // list no files; the commits they bring in count on their own.
  const log = git('log', '-z', '--no-merges', '--format=%x01%B', '--name-only', previous + '..HEAD');
  const bump = log.split('\x01').slice(1).reduce((highest, record) => {
    const [message, ...files] = record.split('\0').map(part => part.replace(/^\n/, ''));
    return files.some(shipsInRelease) ? Math.max(highest, bumpFor(message)) : highest;
  }, 0);
  if (!bump) return '';
  const parts = previous.slice(1).split('.').map(Number);
  const index = 3 - bump;
  parts[index]++;
  parts.fill(0, index + 1);
  return 'v' + parts.join('.');
}

if (require.main === module) console.log(nextTag());
module.exports = { bumpFor, nextTag, shipsInRelease };
