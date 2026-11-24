# Running CI on your own server

The runner image, Compose file and setup steps live in
[spar/gh-private-runners](https://github.com/spar/gh-private-runners), shared
with my other repositories. This repository's stack there is `dockernext`. It
has no Docker access: CI uses fake Docker engines and does not start service
containers or build images.

## Choosing where CI runs

The switch is the **repository Actions variable `CI_RUNNER`**, under
**Settings, Secrets and variables, Actions, Variables**:

| `CI_RUNNER` | Where jobs run |
|---|---|
| Unset or empty | GitHub-hosted Ubuntu and macOS tests, Ubuntu lint and vulnerability checks |
| `self-hosted` | Your registered Linux runners for tests, lint and vulnerability checks |
| `dockernext` | Your runners with the `dockernext` label |

```sh
gh variable set CI_RUNNER --body self-hosted --repo spar/dpad
gh variable delete CI_RUNNER --repo spar/dpad   # back to GitHub's runners
```

Self-hosted mode tests both Go versions on Linux only; macOS tests come back
when the variable is removed. Jobs wait while all your runners are busy or
offline and do not fall back to GitHub's runners. In self-hosted mode,
`actions/setup-go` skips the GitHub Actions cache, because each runner keeps
its own Go SDKs, module cache, build cache and golangci-lint cache.

## Setting up the runners

On the server, with gh-private-runners cloned (see its README):

```sh
./runners dockernext register
```

If you ran the runners from this repository's old `runner/` folder, stop them
there with `docker compose down` (no `-v`) and start them with
`./runners dockernext up -d --build`. The names and volumes are the same, so
they keep their registration.
