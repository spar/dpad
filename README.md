# dockernext (`dn`)

A colorful, interactive companion to the `docker` CLI. It adds colors and
sorting to the commands you use most, and a full-screen TUI. It does not replace
`docker`: anything `dn` doesn't enhance is passed straight through.

## Getting started

You need Go 1.26.7+ and a running Docker engine (Docker Desktop, colima,
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
| `dn` | Full-screen TUI: Containers / Images / Stats / Projects tabs, filtering, container details, follow logs, container shells, start/stop/restart |
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
| `tab` / `shift+tab` / `1-4` | switch tab |
| `↑↓` `j k` `pgup pgdn` `g G` | move |
| `/` | filter the current tab (`enter` keeps, `esc` clears) |
| `s` / `r` | cycle sort key / reverse |
| `d` | open details of the selected container (`esc` back) |
| `e` | choose a shell executable for the selected container (`enter` opens, `esc` cancels) |
| `enter` | follow logs of the selected container (`esc` back) |
| `space` (Projects) | expand/collapse the selected project |
| `enter` (project row) | follow logs across the selected project (`esc` back) |
| `S` / `x` / `R` | start (or unpause) / stop / restart (asks y/N) |
| `ctrl+r` | refresh |
| `?` | help |
| `q` | quit |

Container details show state, exit code, OOM status, restart count, image,
entrypoint, command, restart policy, health-check results, ports, networks, and
mounts. Scroll with `↑↓` / `j k`, `pgup` / `pgdn`, or `g` / `G`; use `←→` to pan
long lines and `ctrl+r` to refresh the snapshot. Press `enter` to follow logs,
then `esc` to return to details. From details, `esc` or `q` returns to the list
with its filter, sort, and selection preserved. Environment variables are omitted.

Press `e` from a container row in Containers, Stats, or Projects, or from its
details pane, to open a shell. The prompt defaults to `/bin/sh`; replace it with
an executable available in the container, such as `/bin/bash` or `powershell.exe`.
Enter only the executable, without arguments. Docker must be installed in PATH,
and the container must be running and not paused or restarting. This runs
`docker exec -it CONTAINER EXECUTABLE` using the Docker CLI's environment and
context settings. Images without a shell need an executable they actually contain.

The shell takes over the terminal. Type `exit` or press `ctrl+d` to return to the
same dashboard view, filter, sort, and selection; container lists and open details
refresh afterward. Errors appear in the status, including recent Docker stderr
when the process fails. Cancelling the prompt leaves the dashboard unchanged.

In the log viewer, press `/` to search and filter buffered logs with a regular
expression. Matches are highlighted and only matching lines are shown, including
new lines as they arrive. Use `(?i)` for case-insensitive matching, for example
`(?i)error|timeout`. Press `enter` to keep the filter, `esc` while editing to
cancel changes, or `ctrl+l` outside the prompt to clear it. Invalid expressions
show an error and keep the last valid results. Filtering searches the current
buffer (up to 5,000 lines); it does not fetch older logs. `esc` or `q` outside
the prompt returns to the previous view.

The Projects tab groups containers by their Compose project and service labels,
with a running/total count and an unhealthy count for each project. Project state
is `running` when all containers run, `partial` when some run, and `inactive`
when none run. Containers without Compose project labels remain in Containers.
Project lists update with Docker events and the regular container refresh.
Filtering by a project, service, container name/ID, image, or state keeps the full
matching project visible so its counts and log targets stay consistent.

Press `space` on a project row to collapse or expand it, or `enter` to follow its
containers' logs with colored name prefixes. Lines appear as they arrive; regex
filtering and scrolling work as in single-container logs. Each container starts
with its last 500 lines, sharing the 5,000-line viewer buffer. Log sources are the
project's containers when the viewer opens; reopen it after recreating or adding
containers. Source errors appear in the status while other streams continue.
Select an individual container row for details, logs, or confirmed lifecycle
actions; project rows do not perform bulk start/stop/restart actions.

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
make lint                          # golangci-lint (config in .golangci.yml)
make vuln                          # govulncheck
make run ARGS="ps -a"              # run from source
```
See [docs/local-development.md](docs/local-development.md#5-development-loop).
