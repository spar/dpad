# dockernext (`dn`)

A colorful, interactive companion to the `docker` CLI. It adds colors and
sorting to the commands you use most, and a full-screen TUI. It does not replace
`docker`: anything `dn` doesn't enhance is passed straight through.

## Getting started

You need Go 1.26+ and a running Docker engine (Docker Desktop, colima,
OrbStack, or Linux).

```sh
make install          # builds and installs dockernext + a `dn` symlink into ~/.local/bin
dn                    # interactive TUI
dn stats --sort mem   # live, sortable stats

make demo-up          # optional: ~20 demo containers that exercise every feature
make demo-down
```

- **[Running locally](docs/local-development.md):** prerequisites, building, installing, the demo environment, and troubleshooting.
- **[Demo environment](testenv/README.md):** what each demo container shows, plus a guided feature tour.

## Commands

| Command | What it adds over docker |
|---|---|
| `dn` | Full-screen TUI: Containers / Images / Stats tabs, filtering, follow logs, start/stop/restart |
| `dn ps [-a] [--sort name\|created\|status] [-r]` | Shows state colors (running, exited ok/error, paused, restarting), a health badge, and compact `8080→80/tcp` ports |
| `dn images [--sort size\|created\|name] [-r]` | Shows human sizes and a total-size footer; dangling images are dimmed |
| `dn stats [--sort cpu\|mem\|net\|io\|pids\|name] [-r] [--no-stream]` | Live sorting (keys `c m n i p a`, `r` to reverse), with CPU and memory colored by threshold plus usage bars |
| `dn logs [-f] [-n N] [-t] [--since T] [--grep RE] C1 [C2...]` | Interleaves several containers with colored name prefixes, highlights log levels (plain, JSON, logfmt, and nginx formats), marks stderr with `┃`, and highlights `--grep` matches |

Sorts default to the natural direction for each key: biggest first for numbers,
A→Z for names. `-r` reverses.

### Passthrough
These cases go to `docker` unchanged:
- Unknown commands: `dn network ls`, `dn exec -it web sh`
- Flags `dn` doesn't implement: `dn ps --format '{{.Names}}'`, `dn ps -q`
- Docker global flags: `dn --context prod ps`

`dn stats web db` also passes through, because docker already handles selecting specific containers.

### TUI keys
| Key | Action |
|---|---|
| `tab` / `shift+tab` / `1-3` | switch tab |
| `↑↓` `j k` `pgup pgdn` `g G` | move |
| `/` | filter the current tab (`enter` keeps, `esc` clears) |
| `s` / `r` | cycle sort key / reverse |
| `enter` | follow logs of the selected container (`esc` back) |
| `S` / `x` / `R` | start (or unpause) / stop / restart (asks y/N) |
| `ctrl+r` | refresh |
| `?` | help |
| `q` | quit |

## Colors
Output is plain when piped, when `NO_COLOR` is set, or with `--no-color`.

## Daemon connection
`dn` honors `DOCKER_HOST`, `DOCKER_CONTEXT`, and the current docker context
(`docker context use …`), so it talks to the same daemon as `docker`. That
includes colima, Docker Desktop, and remote contexts.

## Layout
```
cmd/                  cobra commands, passthrough routing
internal/docker/      Engine API client, stats math, log streaming
internal/render/      palette, ANSI-aware table, formatters, log highlighting
internal/sortx/       sort keys shared by CLI and TUI
internal/tui/         Bubble Tea app and the live stats view
internal/passthrough/ exec docker for everything else
testenv/              demo docker compose environment
docs/                 guides
```

## Development
```sh
make test                          # unit tests: stats math, sorting, formatting, level detection
make vet
make run ARGS="ps -a"              # run from source
```
See [docs/local-development.md](docs/local-development.md#5-development-loop).
