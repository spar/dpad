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
| `dn` | Full-screen TUI: Containers / Images / Stats / Projects tabs, stats history, filtering, container/image details, logs, shells, confirmed actions |
| `dn ps [-a] [--sort name\|created\|status] [-r]` | Shows state colors (running, exited ok/error, paused, restarting), a health badge, and compact `8080→80/tcp` ports |
| `dn images [--sort size\|created\|name] [-r]` | Shows human sizes and a total-size footer; dangling images are dimmed |
| `dn stats [--sort cpu\|mem\|net\|io\|pids\|name] [-r] [--no-stream] [--totals]` | Live sorting, CPU/memory usage bars, network/disk throughput; `t` toggles I/O rates/totals |
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
| `t` (Stats) | toggle network/disk rates and cumulative totals; sorting follows the mode |
| `d` | open details of the selected container or image (`esc` back) |
| `e` | choose a shell executable for the selected container (`enter` opens, `esc` cancels) |
| `enter` | follow logs of the selected container (`esc` back) |
| `space` (Projects) | expand/collapse the selected project |
| `enter` (project row) | follow logs across the selected project (`esc` back) |
| `enter` (Images) | inspect the selected image |
| `x` (Images or image details) | confirm removal of the selected tag or image |
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

In Images, the usage column counts containers by image ID, including stopped
containers. `unused` means none reference the image; a failed usage refresh shows
`unknown`, not `unused`. Press `d` or `enter` for tags, digests, creation date,
OS/architecture, size, configuration, labels, and the names/states of containers
using the image. Environment variables are omitted. Scroll and pan as in container
details, `ctrl+r` refreshes, and `esc` or `q` returns to Images.

Press `x` on an image row or in image details to remove the selected reference,
then `y` to confirm. With multiple tags, this removes only the selected tag and
keeps other tags and image data. Removing the last tag or an untagged image may
delete image data; the confirmation states which operation will happen. Deletion
is refused while any running or stopped container references the image. Tag
removal from a multi-tag image still follows Docker's normal conflict checks.
Image identity and tag state are rechecked after confirmation; changes require
a refresh and a new confirmation. Removal never forces deletion or prunes parent
images. Success returns to the refreshed list with the same filter/sort and
selection, or the adjacent row when the selected reference was removed. Image
sizes include shared layers and are not a guarantee of disk space reclaimed.

The TUI Stats tab shows CPU and memory sparklines for the selected container,
with average and peak usage over the last two minutes of received samples.
History starts when Stats is first opened and stays in memory for this session.
Each container has 120 fixed one-second buckets; samples within a second are
aggregated, and compressed chart columns retain their peaks so short spikes stay
visible. Blank columns mean no samples in that interval; dots mean measured zero.
CPU scales to at least 100% and expands for multi-core usage; memory scales to its
recent peak. Both scales are shown below the charts. Averages use received
samples and do not fill missing intervals. Restarted/reconnected streams start
fresh history, and stopped containers' history is discarded.

The panel follows selection through sorting/filtering. It hides automatically
below 80 columns or 10 body rows. Standalone `dn stats`, snapshots, and piped
output omit history storage. History uses
about 6 KiB per tracked container plus map overhead, adds no Docker requests,
and retains the existing batched stats redraws; only the selected container's
chart is rendered.

Live Stats shows network RX/TX and disk read/write throughput in bytes/sec.
Press `t` to toggle cumulative byte totals; `dn stats --totals` starts standalone
live stats in totals mode. `n` and `i` sort by combined network/disk throughput
in rates mode, or cumulative bytes in totals mode. Unavailable rates sort after
measured rates even when sorting is reversed. Snapshots (`--no-stream`) and piped
output always show and sort by totals because throughput requires two samples.

Rates are interval averages from daemon timestamps and counter differences, not
lifetime totals divided by uptime. First samples, missing timestamps/counters,
counter resets, changes in the number of interfaces or disk counters, and gaps
over ten seconds show
`—` while establishing a new baseline. Non-increasing timestamps are ignored for
rate calculation. A valid idle interval shows `0B/s`; unavailable network or disk
counters remain `—`. Each new stats stream starts fresh. Calculation adds no
Docker requests, timers, or growing buffers.

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
