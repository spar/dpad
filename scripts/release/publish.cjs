// Keep the release in draft until every archive and the checksum file is present.
const fs = require('node:fs');
const path = require('node:path');

module.exports = async function publish({ github, context, tag, dir }) {
  const api = github.rest.repos;
  const repo = context.repo;
  const releases = await github.paginate(api.listReleases, { ...repo, per_page: 100 });
  let release = releases.find(r => r.tag_name === tag);
  if (release && !release.draft) {
    throw new Error(tag + ' has already been published');
  }

  const target = { ...repo, tag_name: tag, target_commitish: context.sha };
  const notes = await api.generateReleaseNotes(target);
  const fields = { ...target, name: tag, body: notes.data.body };
  if (release) {
    await api.updateRelease({ ...fields, release_id: release.id });
  } else {
    release = (await api.createRelease({ ...fields, draft: true })).data;
  }

  const release_id = release.id;
  const assets = await github.paginate(api.listReleaseAssets, { ...repo, release_id });
  for (const name of fs.readdirSync(dir)) {
    const old = assets.find(asset => asset.name === name);
    if (old) await api.deleteReleaseAsset({ ...repo, asset_id: old.id });
    const data = fs.readFileSync(path.join(dir, name));
    await api.uploadReleaseAsset({
      ...repo, release_id, name, data,
      headers: { 'content-type': 'application/octet-stream', 'content-length': data.length },
    });
  }
  await api.updateRelease({ ...target, release_id, draft: false, make_latest: 'true' });
};
