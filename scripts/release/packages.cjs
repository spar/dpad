// Render package-manager manifests for a published release from its SHA256SUMS,
// and push the Homebrew formula and Scoop manifest to their repositories.
const fs = require('node:fs');
const path = require('node:path');

const repo = 'spar/dpad';
const desc = 'Colorful, interactive companion to the docker CLI';
const homepage = 'https://github.com/' + repo;

function parseSums(text) {
  const sums = {};
  for (const line of text.split('\n')) {
    const m = /^([0-9a-f]{64}) [ *]?(\S+)$/.exec(line.trim());
    if (m) sums[m[2]] = m[1];
  }
  return sums;
}

function asset(tag, sums, name) {
  const sha256 = sums[name];
  if (!sha256) throw new Error('SHA256SUMS has no entry for ' + name);
  return { url: `${homepage}/releases/download/${tag}/${name}`, sha256 };
}

function versionOf(tag) {
  if (!/^v\d+\.\d+\.\d+$/.test(tag)) throw new Error('unexpected release tag ' + tag);
  return tag.slice(1);
}

function formula(tag, sums) {
  const block = (os, arch) => {
    const a = asset(tag, sums, `dpad-${os}-${arch}.tar.gz`);
    return `      url "${a.url}"\n      sha256 "${a.sha256}"`;
  };
  return `# Updated by ${repo}'s release workflow.
class Dpad < Formula
  desc "${desc}"
  homepage "${homepage}"
  version "${versionOf(tag)}"
  license "MIT"

  on_macos do
    on_arm do
${block('darwin', 'arm64')}
    end
    on_intel do
${block('darwin', 'amd64')}
    end
  end

  on_linux do
    on_arm do
${block('linux', 'arm64')}
    end
    on_intel do
${block('linux', 'amd64')}
    end
  end

  def install
    bin.install "dpad"
    bin.install_symlink "dpad" => "dp"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/dp --version")
  end
end
`;
}

function scoop(tag, sums) {
  const arch = name => {
    const a = asset(tag, sums, name);
    return { url: a.url, hash: a.sha256 };
  };
  const download = name => `${homepage}/releases/download/v$version/${name}`;
  const manifest = {
    version: versionOf(tag),
    description: desc,
    homepage,
    license: 'MIT',
    architecture: {
      '64bit': arch('dpad-windows-amd64.zip'),
      arm64: arch('dpad-windows-arm64.zip'),
    },
    bin: ['dpad.exe', ['dpad.exe', 'dp']],
    checkver: 'github',
    autoupdate: {
      architecture: {
        '64bit': { url: download('dpad-windows-amd64.zip') },
        arm64: { url: download('dpad-windows-arm64.zip') },
      },
      hash: { url: '$baseurl/SHA256SUMS' },
    },
  };
  return JSON.stringify(manifest, null, 2) + '\n';
}

// AUR's archives get versioned local names because the release asset names are fixed.
function aurSources(tag, sums) {
  const version = versionOf(tag);
  return [['x86_64', 'amd64'], ['aarch64', 'arm64']].map(([arch, goarch]) => {
    const a = asset(tag, sums, `dpad-linux-${goarch}.tar.gz`);
    return { arch, source: `dpad-${version}-${arch}.tar.gz::${a.url}`, sha256: a.sha256 };
  });
}

function pkgbuild(tag, sums) {
  const sources = aurSources(tag, sums);
  const lines = sources.flatMap(s => [
    `source_${s.arch}=("${s.source}")`,
    `sha256sums_${s.arch}=('${s.sha256}')`,
  ]);
  return `# Maintainer: spar <${homepage}>
# Updated by ${repo}'s release workflow.
pkgname=dpad-bin
pkgver=${versionOf(tag)}
pkgrel=1
pkgdesc="${desc}"
arch=('x86_64' 'aarch64')
url="${homepage}"
license=('MIT')
optdepends=('docker: runs the commands dp passes through')
provides=('dpad')
conflicts=('dpad')
${lines.join('\n')}

package() {
  install -Dm755 dpad "$pkgdir/usr/bin/dpad"
  ln -s dpad "$pkgdir/usr/bin/dp"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
`;
}

// The same fields makepkg --printsrcinfo would emit for pkgbuild().
function srcinfo(tag, sums) {
  const sources = aurSources(tag, sums).flatMap(s => [
    `\tsource_${s.arch} = ${s.source}`,
    `\tsha256sums_${s.arch} = ${s.sha256}`,
  ]);
  return `pkgbase = dpad-bin
\tpkgdesc = ${desc}
\tpkgver = ${versionOf(tag)}
\tpkgrel = 1
\turl = ${homepage}
\tarch = x86_64
\tarch = aarch64
\tlicense = MIT
\toptdepends = docker: runs the commands dp passes through
\tprovides = dpad
\tconflicts = dpad
${sources.join('\n')}

pkgname = dpad-bin
`;
}

async function putFile(github, { owner, repo, path, content, message }) {
  let sha;
  try {
    const { data } = await github.rest.repos.getContent({ owner, repo, path });
    if (Buffer.from(data.content, 'base64').toString('utf8') === content) return false;
    sha = data.sha;
  } catch (err) {
    if (err.status !== 404) throw err;
  }
  await github.rest.repos.createOrUpdateFileContents({
    owner, repo, path, message, sha, content: Buffer.from(content).toString('base64'),
  });
  return true;
}

// github must be authenticated with a token that can write to both repositories.
async function publishManifests({ github, core, tag, sumsFile }) {
  const sums = parseSums(fs.readFileSync(sumsFile, 'utf8'));
  const owner = repo.split('/')[0];
  const message = `dpad ${versionOf(tag)}`;
  for (const target of [
    { repo: 'homebrew-tap', path: 'Formula/dpad.rb', content: formula(tag, sums) },
    { repo: 'scoop-bucket', path: 'bucket/dpad.json', content: scoop(tag, sums) },
  ]) {
    const changed = await putFile(github, { owner, message, ...target });
    core.info(`${owner}/${target.repo}/${target.path}: ${changed ? 'updated' : 'already current'}`);
  }
}

const winget = { id: 'spar.dpad', schema: '1.28.0', upstream: { owner: 'microsoft', repo: 'winget-pkgs' } };

// Multi-file manifest for microsoft/winget-pkgs. winget allows one alias per
// file, so the portable install is exposed as `dp`, the command the docs use.
function wingetManifests(tag, sums) {
  const version = versionOf(tag);
  const head = (type) => `# yaml-language-server: $schema=https://aka.ms/winget-manifest.${type}.${winget.schema}.schema.json

PackageIdentifier: ${winget.id}
PackageVersion: ${version}
`;
  const tail = (type) => `ManifestType: ${type}\nManifestVersion: ${winget.schema}\n`;
  const installer = (arch, goarch) => {
    const a = asset(tag, sums, `dpad-windows-${goarch}.zip`);
    return `- Architecture: ${arch}\n  InstallerUrl: ${a.url}\n  InstallerSha256: ${a.sha256.toUpperCase()}\n`;
  };
  const dir = `manifests/${winget.id[0]}/${winget.id.replace('.', '/')}/${version}`;
  return {
    dir,
    files: {
      [`${winget.id}.yaml`]: head('version') + `DefaultLocale: en-US\n` + tail('version'),
      [`${winget.id}.installer.yaml`]: head('installer') + `InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
- RelativeFilePath: dpad.exe
  PortableCommandAlias: dp
Commands:
- dp
Installers:
${installer('x64', 'amd64')}${installer('arm64', 'arm64')}` + tail('installer'),
      [`${winget.id}.locale.en-US.yaml`]: head('defaultLocale') + `PackageLocale: en-US
Publisher: spar
PublisherUrl: https://github.com/spar
PublisherSupportUrl: ${homepage}/issues
PackageName: dpad
PackageUrl: ${homepage}
License: MIT
LicenseUrl: ${homepage}/blob/HEAD/LICENSE
ShortDescription: ${desc}
Description: dp adds live stats with sparklines, multi-container logs, Compose project controls and a full-screen terminal UI to the docker CLI. Commands it does not enhance are passed through to docker, which must be installed.
Moniker: dpad
Tags:
- cli
- containers
- docker
- docker-compose
- terminal
- tui
ReleaseNotesUrl: ${homepage}/releases/tag/${tag}
` + tail('defaultLocale'),
    },
  };
}

// Commits the manifests to a branch of the fork, cut from upstream's master,
// and opens a pull request against microsoft/winget-pkgs. github must be
// authenticated as the fork's owner.
async function publishWinget({ github, core, tag, sumsFile, fork }) {
  const sums = parseSums(fs.readFileSync(sumsFile, 'utf8'));
  const { dir, files } = wingetManifests(tag, sums);
  const version = versionOf(tag);
  const up = winget.upstream;
  const exists = async (path) => {
    try {
      await github.rest.repos.getContent({ ...up, path });
      return true;
    } catch (err) {
      if (err.status === 404) return false;
      throw err;
    }
  };
  if (await exists(dir)) {
    core.info(`${winget.id} ${version} is already in ${up.owner}/${up.repo}`);
    return;
  }
  const branch = `${winget.id}-${version}`;
  // winget moderators review one pending submission per package; a later
  // release waits until the open one merges or is closed.
  const q = `repo:${up.owner}/${up.repo} is:pr is:open author:${fork} "${winget.id}" in:title`;
  const pending = (await github.rest.search.issuesAndPullRequests({ q })).data.items[0];
  if (pending) {
    core.info(`Pull request already open for ${winget.id}: ${pending.html_url}`);
    return;
  }

  const base = (await github.rest.git.getRef({ ...up, ref: 'heads/master' })).data.object.sha;
  const repo = { owner: fork, repo: up.repo };
  const tree = await github.rest.git.createTree({
    ...repo,
    base_tree: base,
    tree: Object.entries(files).map(([name, content]) => ({ path: `${dir}/${name}`, mode: '100644', type: 'blob', content })),
  });
  const isNew = !(await exists(path.posix.dirname(dir)));
  const title = isNew ? `New package: ${winget.id} version ${version}` : `New version: ${winget.id} version ${version}`;
  const commit = await github.rest.git.createCommit({ ...repo, message: title, tree: tree.data.sha, parents: [base] });
  try {
    await github.rest.git.createRef({ ...repo, ref: `refs/heads/${branch}`, sha: commit.data.sha });
  } catch (err) {
    if (err.status !== 422) throw err;
    await github.rest.git.updateRef({ ...repo, ref: `heads/${branch}`, sha: commit.data.sha, force: true });
  }
  const pr = await github.rest.pulls.create({
    ...up,
    title,
    head: `${fork}:${branch}`,
    base: 'master',
    body: `## Description\n\n${title}, from the ${repo.owner}/dpad release ${tag} (${homepage}/releases/tag/${tag}).\n\nSubmitted by the dpad release workflow.`,
  });
  core.info(`Opened ${pr.data.html_url}`);
}

function writeAur({ tag, sumsFile, dir }) {
  const sums = parseSums(fs.readFileSync(sumsFile, 'utf8'));
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'PKGBUILD'), pkgbuild(tag, sums));
  fs.writeFileSync(path.join(dir, '.SRCINFO'), srcinfo(tag, sums));
}

module.exports = { parseSums, formula, scoop, pkgbuild, srcinfo, wingetManifests, publishManifests, publishWinget, writeAur };
