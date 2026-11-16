# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project

dockernext (`dn`) is a Go CLI and terminal UI that enhances the Docker CLI.
`ps`, `images`, `stats` and `logs` are reimplemented against the Docker Engine
API; any other command or flag is passed through to the `docker` binary. Running
`dn` with no arguments opens a full-screen TUI built on Bubble Tea, Bubbles and
Lip Gloss v2.

Setup instructions for macOS, Linux and Windows are in
[docs/local-development.md](docs/local-development.md).

## Layout

| Path | Contents |
|---|---|
| `main.go` | entry point |
| `cmd/` | cobra commands and the routing that sends unknown commands to `docker` |
| `internal/docker/` | Engine API client, docker-context resolution, stats math, events, log streaming |
| `internal/passthrough/` | executes the real `docker` binary |
| `internal/render/` | theme, ANSI-aware table, formatters, log-level highlighting |
| `internal/sortx/` | sort keys shared by the CLI and the TUI |
| `internal/tui/` | the Bubble Tea app (`app.go`) and its views |
| `testenv/` | demo compose environment (`make demo-up`) |
| `scripts/screenshots/` | regenerates `docs/images` from the demo (`make screenshots`) |

## Commands

```sh
make build    # → bin/dockernext
make test
make vet
make lint     # golangci-lint, version pinned in the Makefile
make vuln     # govulncheck
```

Run `make test vet lint` before considering a change done. CI runs the same
checks (`.github/workflows/`).

## Code style

- Follow `gofmt` and the linters configured in `.golangci.yml`.
- Keep comments rare; explain why, not what.
- Don't add abstractions, options or error handling the change doesn't need.
- Add or update tests next to the code you change (`*_test.go`). Tests use fakes
  (`fake_test.go`) rather than a live Docker daemon.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <imperative summary>

<optional body explaining why>
```

Types: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `build`, `ci`,
`chore`. Scope is the area touched, e.g. `tui`, `stats`, `render`, `docker`.
Mark breaking changes with `!` after the type/scope.

Examples:

```
fix(stats): show 0% CPU for a stream's first sample
feat(tui): add an images view
docs: add Linux and Windows setup steps
```

Keep each commit to one logical change.
