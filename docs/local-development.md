# Running dockernext locally

This guide takes you from a fresh clone to running `dn` against a demo set of
containers.

- [1. Prerequisites](#1-prerequisites)
- [2. Build and run from source](#2-build-and-run-from-source)
- [3. Install `dn` on your PATH](#3-install-dn-on-your-path)
- [4. Start the demo environment](#4-start-the-demo-environment)
- [5. Development loop](#5-development-loop)
- [6. Troubleshooting](#6-troubleshooting)

## 1. Prerequisites

| Tool | Version | Check | Needed for |
|---|---|---|---|
| Go | 1.26+ | `go version` | building |
| Docker Engine | any recent | `docker info` | everything. Docker Desktop, colima, OrbStack and Linux all work |
| `docker` CLI | any | `docker version` | passthrough of commands `dn` doesn't enhance |
| Docker Compose v2 | any | `docker compose version` or `docker-compose version` | the demo environment only |
| `make` | any | `make --version` | convenience targets (optional) |

**On macOS with Homebrew:**
```sh
brew install go docker docker-compose
# Pick one engine:
brew install colima && colima start --cpu 4 --memory 4   # or install Docker Desktop / OrbStack
```

**Compose with colima:** `brew install docker-compose` installs a standalone
binary. To use it as `docker compose`, register Homebrew's plugin directory in
`~/.docker/config.json`:
```json
{
  "cliPluginsExtraDirs": ["/opt/homebrew/lib/docker/cli-plugins"]
}
```
The Makefile already falls back to `docker-compose` when the plugin isn't
available, so this step is optional.

## 2. Build and run from source

```sh
cd dockernext
make build                 # → bin/dockernext
./bin/dockernext ps -a     # try it
./bin/dockernext           # full-screen TUI (q to quit)
```

Without `make`:
```sh
go build -o bin/dockernext .
```

To run straight from source without building, use `make run`:
```sh
make run ARGS="stats --sort mem"
go run . logs -f my-container      # same thing without make
```

`dn` uses whatever daemon `docker` uses (`DOCKER_HOST`, `DOCKER_CONTEXT`, or the
current `docker context`). As a quick sanity check, `./bin/dockernext ps -a` should
list the same containers as `docker ps -a`.

## 3. Install `dn` on your PATH

```sh
make install                 # → ~/.local/bin/dockernext and ~/.local/bin/dn
```

If `~/.local/bin` isn't on your PATH yet (zsh shown; use `~/.bashrc` for bash):
```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
dn --version
```

**Other ways to install:**
- System-wide: `sudo make install PREFIX=/usr/local`
- With `go install .`: this puts `dockernext` in `$(go env GOPATH)/bin`. Add `alias dn=dockernext` to your shell rc file.
- **Shell completion:** for example, `dn completion zsh > "${fpath[1]}/_dn"`. See `dn completion --help`.

To remove it, run `make uninstall` (use the same `PREFIX` you installed with).

## 4. Start the demo environment

`testenv/compose.yaml` starts about 20 containers, each designed to exercise one
dn feature. Among them are a CPU hog, a container near its memory limit, an
unhealthy one, a crash-looping one, a paused one, several log formats, and
network and disk I/O generators.

```sh
make demo-up        # pulls alpine, nginx, python (~165MB total) and starts everything
dn                  # explore. The demo containers are all named dn-demo-*
make demo-down      # removes containers, the volume, and the dangling demo image
```

`make demo-up` also creates three things compose can't express: a **paused**
container, a **created** (never started) container, and a **dangling** image.

See **[testenv/README.md](../testenv/README.md)** for what each container
demonstrates and a step-by-step feature tour.

> **Resource use:** `cpu-hog` and `cpu-warm` together use about 1.5 CPU cores
> and `traffic` moves about 5MB/s over the local bridge network. To pause just
> those while keeping the rest:
> `docker compose -f testenv/compose.yaml stop cpu-hog cpu-warm traffic`

**Other demo targets:**

| Target | Does |
|---|---|
| `make demo-status` | `docker compose ps -a` for the demo project |
| `make demo-extras` | re-create the paused, created and dangling extras |
| `make demo-up COMPOSE=docker-compose` | force a specific compose binary |

## 5. Development loop

```sh
make test      # unit tests: stats math, sorting, formatting, log-level detection
make vet
make build && ./bin/dockernext stats
```

**Where things live:**

| Path | Contents |
|---|---|
| `cmd/` | cobra commands (`ps`, `images`, `stats`, `logs`) and the routing that sends unknown commands or flags to `docker` |
| `internal/docker/` | Engine API client, docker-context resolution, stats math, log streaming |
| `internal/render/` | colors, the ANSI-aware table, formatters, log-level highlighting |
| `internal/sortx/` | sort keys shared by the CLI and the TUI |
| `internal/tui/` | the Bubble Tea app (`app.go`) and its tabs |
| `testenv/` | demo compose file and the scripts its containers run |

**Tips:**
- **Colors and piping:** colors are disabled automatically when output is piped, so `dn ps | less` shows plain text. Run `dn ps` directly in a terminal to see colors.
- **Re-running the TUI:** after a change, `go run .` is the fastest way to try it.
- **Version string:** `make build` stamps it from `git describe`. Other builds (`go build`, `go install`) report the module version Go records, and `dev` if there is none.

## 6. Troubleshooting

| Symptom | Fix |
|---|---|
| `Cannot connect to the Docker daemon` | Start your engine (`colima start`, or open Docker Desktop). Check `docker context ls`: `dn` follows the context marked `*`. |
| `dn ps` shows different containers than `docker ps` | `DOCKER_HOST` set in one shell but not the other? `dn` honors `DOCKER_HOST` > `DOCKER_CONTEXT` > the current context. |
| No colors | Output is piped or redirected, `NO_COLOR` is set, or you passed `--no-color`. |
| A row shows `0.00%` CPU for a moment | The first sample of a newly started container has no previous sample to diff against. It fills in after about 1s. |
| `dn: docker CLI not found in PATH` | Passthrough needs the `docker` binary. Enhanced commands (`ps`, `images`, `stats`, `logs`, the TUI) work without it. |
| `docker: unknown command: docker compose` | Compose isn't installed as a plugin. See [Prerequisites](#1-prerequisites), or run `make demo-up COMPOSE=docker-compose`. |
| Demo containers fail with `/scripts/...: not found` | The engine can't see the repo directory. colima and Docker Desktop share your home directory by default, so clone under `~` or add the path to the engine's file-sharing settings. |
| TUI looks garbled | Use a terminal with UTF-8 and 256 colors (iTerm2, the macOS Terminal, kitty, WezTerm, Windows Terminal). Over tmux, set `TERM=tmux-256color`. |
| `go: go.mod requires go >= 1.26.0` | Upgrade Go (`brew upgrade go`). |
