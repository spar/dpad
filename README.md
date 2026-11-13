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
| `dn` | Full-screen TUI: Containers / Images / Stats / Projects / Events / Volumes / Networks tabs, stats history, filtering, resource details, logs, shells, confirmed actions |
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
| `tab` / `shift+tab` / `1-7` | switch tab |
| `↑↓` `j k` `pgup pgdn` `g G` | move |
| `/` | filter the current tab (`enter` keeps, `esc` clears) |
| `s` / `r` | cycle sort key / reverse |
| `t` (Stats) | toggle network/disk rates and cumulative totals; sorting follows the mode |
| `v` (Stats) | toggle Compose projects / flat containers |
| `space` / `enter` (Stats project) | expand/collapse a project; `space` on a member returns to its project |
| `d` | open details of the selected container or image (`esc` back) |
| `e` | choose a shell executable for the selected container (`enter` opens, `esc` cancels) |
| `enter` | follow logs of the selected container (`esc` back) |
| `space` (Projects) | expand/collapse the selected project |
| `enter` (project row) | follow logs across the selected project (`esc` back) |
| `enter` (Images) | inspect the selected image |
| `d` / `enter` (Events) | inspect the event's container |
| `d` / `enter` (Volumes) | inspect the selected volume and its container references |
| `d` / `enter` (Networks) | inspect the selected network |
| `tab` (network details) | switch metadata / endpoints; `d` or `enter` on an endpoint opens container details |
| `r` / `g` (Events) | reverse time order / return to newest and follow arrivals |
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
with its last 500 lines, sharing the 5,000-line viewer buffer. Project log sessions
follow Compose labels as containers are added, scaled down, recreated, or
restarted. The existing Docker event subscription and ten-second polling fallback
update membership. Empty projects stay open waiting for new containers.

Interrupted project sources reconnect with backoff of 1, 2, 4, then at most 5
seconds, resuming from the last daemon timestamp. Bounded fingerprint matching
suppresses replay at that timestamp while preserving new lines with the same
timestamp. Recovery depends on logs Docker still retains; replay suppression
tracks up to 256 records at the last timestamp per source. After a successful
history read, stopped sources wait for a restart. Source counts, joins, leaves, retries,
and errors appear in the status while healthy sources continue. A failed
container-list refresh retains known sources until discovery recovers.

Membership changes and reconnects preserve the filter, buffered logs, and paused
scroll position while those lines remain in the buffer. Closing the viewer
cancels all readers and pending retries. One timer serves retry scheduling for
the session; snapshots and status updates coalesce, and log batches retain the
existing redraw limit. Colors are stable by container name across recreation.
Select an individual container row for details, logs, or confirmed lifecycle
actions. On a project header in Projects, `S`, `x`, and `R` open a scrollable
start, stop, or restart preview. Press `y` to confirm, or `esc` to return.
Start targets created, exited, and paused containers (unpausing the latter);
stop targets running, paused, and restarting containers; restart targets running
containers only. Other states are skipped. Targets are read again before work
begins; changed names, IDs, services, or states require a new confirmation.

Project actions use existing containers through the Docker Engine API. They do
not create or recreate services, apply Compose files, or order dependencies.
Up to four requests run at once, each with a 45-second timeout. The progress
view shows per-container results; navigate to a failed row for its error.
`esc` during work cancels remaining requests and keeps partial results visible.
An interrupted in-flight request is marked `unknown` because Docker may already
have applied it. Closing the completed view returns to the same project filter
and selection; container lists refresh when work finishes.

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

The Events tab (`5`) records container lifecycle and health events from TUI
startup, including create, start, stop, restart, die, destroy, pause/unpause,
rename, update, kill, OOM, and health changes. Rows show local timestamps,
container names, Compose project/service labels, and exit codes, signals, or
image names when available. No per-event container inspection is needed; names
and project labels come from the event itself. Noisy exec events are excluded.

Use `/` for a case-insensitive substring filter by container name/ID, project,
service, event type, or image. Newest arrivals appear first; navigating keeps
the selected event fixed while new events arrive. `r` reverses order, and `g`
returns to the newest event and follows arrivals. `d` or `enter` opens current
container details; `esc` returns with the timeline filter/order/selection intact.
Destroyed containers can still have events in the timeline, but their details
are unavailable. Shell and lifecycle actions are offered on the current
container lists, rather than historical event rows.

The session keeps the latest 1,000 records in a fixed ring, replacing the oldest
as new records arrive. Collection shares the container-list event subscription,
uses bounded queues, and batches updates at most ten times per second.
Over SSH, event updates batch at most four times per second and log updates at
most ten times per second; log paging moves immediately without animation.
SSH detection uses `SSH_CONNECTION`, `SSH_CLIENT`, or `SSH_TTY`. Table layouts
are reused while navigating unchanged data. Automatic container-list refreshes
coalesce Docker event bursts into at most one request per second; manual refresh
and subscription recovery remain immediate.
Interrupted subscriptions retry with one-to-five-second backoff, retaining the
buffer and view settings. A `stream gap` record marks an interruption: events
missed during disconnection are not replayed. Filters also apply to these gap
records; clear the filter or search `stream gap` to see them. This is an in-memory
session timeline, not a persistent audit log or historical event fetch.

The read-only Volumes tab (`6`) lists named and anonymous volumes with driver,
Compose project, and container usage. It loads on opening the tab and refreshes
when revisited or with `ctrl+r`. Filter with `/` by volume name, driver, project,
or the name/ID of a referencing container. Names sort alphabetically; `r` reverses
the order. In narrow terminals, the project column hides while project filtering
remains available.

Usage counts running and stopped containers reported by the selected daemon.
A container mounting the same volume at several paths counts once, and bind
mounts/tmpfs are excluded. `unreferenced` means no references were reported;
failed container lookups show `unknown`. Usage describes container references,
not disk space, and does not include references on other daemon hosts. Driver
warnings remain visible in the status.

Press `d` or `enter` for driver, scope, creation time, mountpoint, labels, and each
referencing container's state and mount destinations (`ro`/`rw`). Mountpoints are
paths on the daemon host. Scroll/pan as in other details panes; `ctrl+r` refreshes
metadata and shared container usage, and `esc` returns with filter, order, and
selection preserved. Volume removal and pruning are not offered.

The tab reuses the dashboard's container lookup and builds a volume-to-container
index once per container refresh. Opening details adds one volume inspection,
without per-volume container scans, disk-size requests, or extra event streams.

The read-only Networks tab (`7`) lists network name, driver, scope, Compose
project, and attached/configured container counts. `/` filters by network name/ID,
driver, scope, project, or a configured container's name/ID; `r` reverses name
order. In narrow terminals, scope/project columns hide while filtering still
includes those fields.

Counts reuse the dashboard's container data: attached means a nonempty endpoint
ID was reported, while configured also includes stopped/created containers with
network references. Failed container lookups show `unknown`, not zero. These are
snapshots from the selected daemon, not cluster-wide totals; host/none networks
may have configured containers without ordinary attached endpoints.

Press `d` or `enter` for ID, creation time, internal/attachable/ingress flags,
IPv4/IPv6 settings, IPAM subnets/gateways/ranges, and labels. `tab` switches to
the endpoint table with container names and IP/MAC addresses. At narrow widths,
the selected endpoint's IPv6 address appears below the table. Move with the
usual navigation keys and press `d` or `enter` for its container details. `esc`
returns to the same endpoint selection, then to the filtered network list.
Endpoints that have disappeared or do not identify a local container can report
an inspection error.

Network lists load on first opening, tab revisits, and `ctrl+r`; details make one
network inspection request and remain a snapshot until refreshed with `ctrl+r`
(also refreshing shared container data). Metadata scrolls and pans like other
details panes. No per-network container scans or extra event subscriptions are
added. Creating/removing networks and connecting/disconnecting containers are not
offered in the TUI.

The TUI Stats tab opens with collapsed Compose projects, making large container
lists easier to navigate. Project rows summarize CPU, memory usage, I/O, and PIDs;
`space` or `enter` expands a project into service/container rows. Containers without
Compose labels stay visible as individual rows. Press `v` for the flat view and
again to return to projects. Sorting applies to project summaries and to the
containers within each expanded project; expansion and selection survive updates.

`/` matches project, service, container name, or ID. A matching member keeps its
whole project visible, including its summary and other members. This keeps totals
consistent while making a project easy to find. Individual rows retain details,
logs, shells, confirmed actions, and history; project headers only expand/collapse.

The count on each project is available/tracked containers. Active Compose members
waiting for their first sample are shown as unavailable. If any member's sample
is missing or stale, the project summary shows `—` rather than a partial total.
Network/disk rates require valid rates from every member for that component.
Project memory shows summed usage only: limits and percentages are omitted because
containers can share the same host memory limit. I/O values sum container counters
or rates, including traffic between members. Summaries use current samples, not
synchronized measurements. Stopped containers leave as discovery/streams update.

Grouping reuses the dashboard's container labels and existing batched stats stream;
it adds no Docker requests, streams, or per-project history buffers. Failed label
refreshes retain known grouping and mark Compose metadata stale. Standalone
`dn stats`, snapshots, and piped output keep their existing flat behavior.

The TUI Stats tab also shows CPU and memory sparklines for the selected container,
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

The TUI shows its bound context/endpoint and connection state below the tabs.
A single daemon ping runs every two seconds with a two-second timeout. Failed
probes retry with backoff from one to five seconds; recovery refreshes lists and
open details without resetting filters or selection. In list tabs, `ctrl+r` refreshes and
requests an immediate connection check. The endpoint stays fixed for the session;
restart `dn` after switching contexts.

Live Stats retries failed startup and interrupted container streams automatically,
also with one-to-five-second backoff. Samples that stop advancing for five seconds
show `—` and lose their old rates/history; the summary counts available streams
and excludes unavailable values. One shared watchdog restarts streams with no
progress for ten seconds. Recovered streams establish fresh rate/history
baselines. This applies to both the Stats tab and standalone `dn stats`, where
`ctrl+r` retries a failed watcher immediately. Quitting cancels background work.

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

To run CI on two or three private Docker runners instead of GitHub's hosted
runners, see [self-hosted runner setup](docs/self-hosted-runner.md). Set the
repository Actions variable `CI_RUNNER=self-hosted` to enable them; leave it
unset for the existing GitHub-hosted Linux/macOS checks.
