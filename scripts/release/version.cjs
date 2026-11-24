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

function nextTag() {
  const git = (...args) => execFileSync('git', args, { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
  const previous = git('tag', '--list', 'v*', '--sort=-version:refname').split('\n')
    .find(tag => /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(tag));
  if (!previous) return 'v0.1.0';

  // An older CI run can finish after a newer one has published.
  const ancestry = spawnSync('git', ['merge-base', '--is-ancestor', previous, 'HEAD']);
  if (ancestry.status === 1) return '';
  if (ancestry.status !== 0) throw new Error('Could not check release tag ancestry');

  const messages = git('log', '-z', '--format=%B', previous + '..HEAD').split('\0');
  const bump = messages.reduce((highest, msg) => Math.max(highest, bumpFor(msg)), 0);
  if (!bump) return '';
  const parts = previous.slice(1).split('.').map(Number);
  const index = 3 - bump;
  parts[index]++;
  parts.fill(0, index + 1);
  return 'v' + parts.join('.');
}

if (require.main === module) console.log(nextTag());
module.exports = { bumpFor, nextTag };
