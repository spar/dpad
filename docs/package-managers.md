# Package managers

Each release updates these packages from the release's `SHA256SUMS`:

| Package manager | Where it lives | Install command |
|---|---|---|
| Homebrew (macOS, Linux) | [spar/homebrew-tap](https://github.com/spar/homebrew-tap), `Formula/dpad.rb` | `brew install spar/tap/dpad` |
| Scoop (Windows) | [spar/scoop-bucket](https://github.com/spar/scoop-bucket), `bucket/dpad.json` | `scoop bucket add spar https://github.com/spar/scoop-bucket` then `scoop install spar/dpad` |
| winget (Windows) | [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs), `manifests/s/spar/dpad`, not published yet (see [winget](#winget)) | `winget install spar.dpad` |
| AUR (Arch Linux) | `dpad-bin`, not published yet (see [AUR](#aur)) | `yay -S dpad-bin` (or any AUR helper) |

Homebrew, Scoop and the AUR install the prebuilt binaries as `dpad` plus a `dp`
alias. winget allows one command per file, so it installs `dp` only.

## How it works

After CI publishes a release on `main`, the `packages` job in
[`ci.yml`](../.github/workflows/ci.yml) calls
[`packages.yml`](../.github/workflows/packages.yml) with the new tag. That
workflow downloads `SHA256SUMS` from the release and renders the manifests with
[`scripts/release/packages.cjs`](../scripts/release/packages.cjs):

- The Homebrew formula and Scoop manifest are committed to their repositories
  through the GitHub API with the `PACKAGES_TOKEN` secret.
- The winget manifests (schema 1.28.0) are committed to a new branch of the
  `spar/winget-pkgs` fork, cut from upstream `master`, and a pull request is
  opened against microsoft/winget-pkgs with the `WINGET_TOKEN` secret. The step
  does nothing if that version is already upstream or any dpad pull request is
  still open there, so a release made while one is under review is not
  submitted. Run the workflow by hand for the latest tag once it merges.
  winget's moderators review and merge it; that usually takes from a few hours
  to a few days.
- `PKGBUILD` and `.SRCINFO` are pushed to `ssh://aur@aur.archlinux.org/dpad-bin.git`
  with the `AUR_SSH_KEY` secret. The workflow checks the AUR host key against the
  Ed25519 fingerprint published on aur.archlinux.org.

A target whose secret is missing is skipped, so releases keep working before
setup is finished. A manifest that is already current is left alone, so reruns
are safe.

To publish an existing release again, run the workflow by hand:

```sh
gh workflow run packages.yml --repo spar/dpad -f tag=v0.4.0
```

## One-time setup

### Homebrew and Scoop

1. Create two **public** repositories, each with a README so they are not empty:
   `spar/homebrew-tap` and `spar/scoop-bucket`. Homebrew requires the
   `homebrew-` prefix; `brew install spar/tap/dpad` resolves to `spar/homebrew-tap`.
2. Create a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new)
   with resource owner `spar`, repository access limited to those two
   repositories, and the **Contents: Read and write** permission. Note the
   expiry date; the workflow fails with a 401 once it lapses.
3. Save it as an Actions secret in this repository:

   ```sh
   gh secret set PACKAGES_TOKEN --repo spar/dpad
   ```

### winget

1. Fork [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) to
   `spar/winget-pkgs`.
2. Create a [classic personal access token](https://github.com/settings/tokens/new)
   with the `public_repo` scope. Fine-grained tokens cannot open pull requests
   on repositories you don't own. Add the `workflow` scope too if branch creation
   fails with a permissions error, which happens when upstream changed workflow
   files.
3. Save it as an Actions secret:

   ```sh
   gh secret set WINGET_TOKEN --repo spar/dpad
   ```

4. Sign the Microsoft Contributor License Agreement when the bot asks, by
   commenting `@microsoft-github-policy-service agree` on the pull request.
   The bot can label later pull requests `Needs-CLA` again; reply the same way
   or wait for a moderator to clear the label.

### AUR

Not set up yet: AUR account registration was temporarily closed in November 2026.
Until `AUR_SSH_KEY` exists the workflow skips this step.

1. Create an account at [aur.archlinux.org](https://aur.archlinux.org/register).
2. Make a key for CI only and add the public half under **My Account, SSH Public Key**:

   ```sh
   ssh-keygen -t ed25519 -N '' -C dpad-aur-ci -f ~/.ssh/dpad-aur
   cat ~/.ssh/dpad-aur.pub
   ```

3. Save the private half as an Actions secret, then delete the local copy if
   you don't need it:

   ```sh
   gh secret set AUR_SSH_KEY --repo spar/dpad < ~/.ssh/dpad-aur
   ```

The first push creates the `dpad-bin` package under that account.

### Publish the current release

Once the secrets are set, run the workflow for the latest tag (command above).
Later releases update the packages on their own.

## Runner requirements

The job runs on `CI_RUNNER` like the rest of CI (see
[Self-hosted CI runners](self-hosted-runner.md)). It needs `curl` and, for the
AUR, `git`, `ssh`, `ssh-keyscan` and outbound port 22 to aur.archlinux.org.
GitHub's Ubuntu runners have all of these.
