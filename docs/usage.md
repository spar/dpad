# Using dpad

The full reference for the `dp` terminal UI and its connection handling. For an
overview, installation and screenshots, see the [README](../README.md).

- [Keys](#keys)
- [Mouse](#mouse)
- [Containers](#containers): details, shells
- [Logs](#logs)
- [Projects](#projects): grouped view, project logs, start/stop/restart, [image updates](#update-compose-projects)
- [Images](#images)
- [Events](#events)
- [Volumes](#volumes)
- [Networks](#networks)
- [Prune](#prune): stopped containers, unused images, volumes, networks and build cache
- [Stats](#stats): Compose grouping, history sparklines, I/O rates, stream recovery
- [Custom commands](#custom-commands): the config file, key bindings, templates
- [Narrow terminals](#narrow-terminals)
- [Responsiveness](#responsiveness)
- [Colors](#colors): themes, `NO_COLOR`
- [Daemon connection](#daemon-connection): contexts, remote daemons over SSH, connection status, switching contexts


## Keys

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
| `P` / `K` / `D` | pause / kill (SIGKILL) / remove, killing a running container first (asks y/N) |
| `e` (Projects, on a project row) | edit one of the project's Compose files in `$VISUAL`/`$EDITOR`, then review, save and update |
| `u` / `U` (Projects) | preview image updates for the selected project / all discovered projects |
| `X` | preview a prune of stopped containers or unused images, volumes, networks or build cache (asks y) |
| `ctrl+r` | refresh |
| `c` (list tabs) | choose a Docker context for this session |
| `C` | choose a color theme; the screen previews each one (`enter` keeps it and saves it to `config.yaml`, `esc` cancels) |
| `o` | settings: shows the theme, Docker context, mouse setting and config file location; `enter` on a row opens its picker |
| `?` | help |
| `q` | quit |

Keys you bind in the [config file](#custom-commands) appear in `?` help for the
tabs they apply to. When help is taller than the window, `↑`/`↓` and
`pgup`/`pgdn` scroll it and any other key closes it.

The footer lists the keys for the selected row. On a narrow terminal it leaves
out the general ones first (`tab`, `c`, `/`, sort) and always keeps `?` and `q`.
Results such as "restart web: done" clear after a few seconds; errors stay until
the next key.

## Mouse

Click a tab to switch to it, or a row to select it. Double-click a row to do
what `enter` does there: follow logs, open details, or expand a Stats project.
The wheel moves the selection in lists and scrolls logs, details and previews.
Clicking anywhere closes help.

While `dp` has the mouse, selecting text with the terminal usually needs a
modifier key, such as `shift` in most Linux and Windows terminals or `option` in
iTerm2; Terminal.app can switch mouse reporting off from its View menu. To leave
the mouse to the terminal entirely, set `mouse: false` in the
[config file](#custom-commands).

## Containers

### Details

Container details show state, exit code, OOM status, restart count, image,
entrypoint, command, restart policy, health-check results, ports, networks, and
mounts. Scroll with `↑↓` / `j k`, `pgup` / `pgdn`, or `g` / `G`; use `←→` to pan
long lines and `ctrl+r` to refresh the snapshot. Press `enter` to follow logs,
then `esc` to return to details. From details, `esc` or `q` returns to the list
with its filter, sort, and selection preserved. Environment variables are omitted.

### Shells

Press `e` from a container row in Containers, Stats, or Projects, or from its
details pane, to open a shell. The prompt defaults to `/bin/sh`; replace it with
an executable available in the container, such as `/bin/bash` or `powershell.exe`.
Enter only the executable, without arguments. Docker must be installed in PATH,
and the container must be running and not paused or restarting. This runs
`docker exec -it CONTAINER EXECUTABLE` with the dashboard's bound daemon endpoint
and startup environment. Switching the Docker CLI's current context while `dp`
is open does not redirect shells to another daemon. Images without a shell need
an executable they actually contain.

The shell takes over the terminal. Type `exit` or press `ctrl+d` to return to the
same dashboard view, filter, sort, and selection; container lists and open details
refresh afterward. Errors appear in the status, including recent Docker stderr
when the process fails. Cancelling the prompt leaves the dashboard unchanged.

## Logs

In the log viewer, press `/` to search and filter buffered logs with a regular
expression. Matches are highlighted and only matching lines are shown, including
new lines as they arrive. Use `(?i)` for case-insensitive matching, for example
`(?i)error|timeout`. Press `enter` to keep the filter, `esc` while editing to
cancel changes, or `ctrl+l` outside the prompt to clear it. Invalid expressions
show an error and keep the last valid results. Filtering searches the current
buffer (up to 5,000 lines); it does not fetch older logs. `esc` or `q` outside
the prompt returns to the previous view.

Long lines are cut at the edge and `←→` pans across them; press `w` to wrap them
instead. `t` shows each line's timestamp in local time, and `?` lists the log
viewer's keys.

## Projects

The Projects tab groups containers by their Compose project and service labels,
with a running/total count and an unhealthy count for each project. Project state
is `running` when all containers run, `partial` when some run, and `inactive`
when none run. Containers without Compose project labels remain in Containers.
Project lists update with Docker events and the regular container refresh.
Filtering by a project, service, container name/ID, image, or state keeps the full
matching project visible so its counts and log targets stay consistent.

### Project logs

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

### Start, stop and restart a project

Select an individual container row for details, logs, or confirmed lifecycle
actions. On a project header in Projects, `S`, `x`, and `R` open a scrollable
start, stop, or restart preview. Press `y` to confirm, or `esc` to return.
Start targets created, exited, and paused containers (unpausing the latter);
stop targets running, paused, and restarting containers; restart targets running
containers only. Other states are skipped. Targets are read again before work
begins; changed names, IDs, services, or states require a new confirmation.

The `S`/`x`/`R` project controls use existing containers through the Docker Engine
API. They do not create or recreate services, apply Compose files, or order dependencies.
Up to four requests run at once, each with a 45-second timeout. The progress
view shows per-container results; navigate to a failed row for its error.
`esc` during work cancels remaining requests and keeps partial results visible.
An interrupted in-flight request is marked `unknown` because Docker may already
have applied it. Closing the completed view returns to the same project filter
and selection; container lists refresh when work finishes.

### Update Compose projects

This is the TUI version of running `docker compose pull` and then
`docker compose up -d` in each project's folder. From Projects, press `u` on a
project or one of its containers to update that project, or `U` to update all
discovered projects on the current daemon. The Projects footer lists both
keys. The all-project preview includes projects hidden by the list
filter and stopped projects that still have containers. Projects with no
containers cannot be discovered from daemon labels.

The preview lists the projects straight away and reads their Compose files in
the background, four at a time, with an elapsed timer. When reading finishes,
each row says `ready` or why it is blocked. Press `y` to start, from the list or
from the `enter` details page, which shows the execution host, working
directory, Compose files, env files, services, and image tags. Updating can
recreate or start services. Missing or inconsistent Compose labels, unavailable
files, or invalid configuration block that project. Valid projects can still be
started.

Each project runs Compose `pull --include-deps`, followed by
`up -d --no-build --pull never` only if the pull succeeds. Projects run one at a
time. Before each step dpad reads the project's configuration again; if it no
longer matches the preview, that project fails with a message and nothing is
pulled or recreated for it.

Pressing `y` opens live progress straight away. When several projects update,
the top half lists every project with its current step and elapsed time
(`checking configuration`, `pulling images`, `recreating services`), and the
bottom half shows the active project's progress, laid out like
`docker compose pull`: one row per image with its layer count and bytes, and
the layers still downloading or extracting listed under it with a bar. Finished
layers are counted, not listed. When a project has more than eight images or
containers, the finished ones collapse into one line so the view fits the
screen. With Compose 2.32 or later dpad reads Compose's JSON progress, which
says which image each layer belongs to; older versions show layers as one
group. Press `l` to switch between compact progress and the raw multiline log, `↑↓` or
`pgup`/`pgdn` to read scrollback, `G` to follow the active project again,
`[`/`]` to inspect another project's output, and `tab` for the full summary
table. The raw log retains the latest 32 KiB per project.

When the update finishes, each project's RESULT says what happened:
`updated, N recreated`, `already up to date`, or `failed:` with Compose's error
message. A bulk update ends on this summary table with the first failed project
selected; `enter` opens its full output. Closing the view shows the totals in
the Projects status bar. A failed
project does not stop the remaining projects. The update uses the tags already
in the Compose files; it does not upgrade pinned versions (press `e` to edit them), edit files, build
images, run `down`, prune images, or remove volumes. Services without profiles,
services selected by `COMPOSE_PROFILES`, active non-one-off services, and their
Compose dependencies are included. Existing profiled services can therefore be
updated without enabling unrelated profiles.

Source paths come from Compose's `project.working_dir`, `project.config_files`,
and `project.environment_file` container labels. Accessible local files run with
the dashboard's bound Docker endpoint. Otherwise, an `ssh://` endpoint can run
Compose in the labeled directory on that SSH host, including rootless socket
paths. The preview states which machine will execute the update. Press `h` in
the preview to toggle local/SSH execution for a project when both machines have
matching paths. Remote execution requires a POSIX shell, Compose v2 or later,
and SSH key/agent authentication
without interactive prompts. Local execution supports either `docker compose`
or standalone Compose v2 (`docker-compose`). TCP-only remote daemons require the
files to be available locally.

Compose uses the labeled env-file overrides, or its normal `.env` discovery
when no override was recorded. Docker does not record shell variables that a
wrapper script exported when it ran Compose (for example `RUNNER_STACK` set by
`./runners <stack>`). When the Compose files need such a variable, dpad looks
for it in the environment of the project's containers and uses it if every
container that sets it agrees on one value. The preview row then says
`ready · sets RUNNER_STACK`, and `enter` shows the value and where it came
from. Variables Compose would leave blank are shown as `left blank` so a
missing value is not applied silently.

When a required variable cannot be recovered, the project is blocked with
Compose's own hint, such as `needs RUNNER_STACK: Run this through ./runners
<stack> so RUNNER_STACK is set`, and `enter` shows how to set it in
`config.yaml`:

```yaml
composeProjects:
  expense-tracker-runner:   # Compose project name
    env:
      RUNNER_STACK: expense-tracker
```

Values from `config.yaml` take precedence over recovered ones. Unrecorded
launch flags cannot be recovered. Resolved configuration is rechecked before
pulling and again before `up`; a change fails that project and needs a fresh
preview. Configuration values are
hashed rather than displayed or retained as logs.

Press `esc` from the live console or project list to cancel remaining work. In-flight
recreation may already have applied, and an SSH command may still be finishing;
interrupted `up` is marked `unknown`. Each project has a 20-minute timeout.
Return to the same filtered Projects view after completion to see refreshed state.

### Edit a Compose file

For a quick change, such as bumping an image tag, press `e` on a project row in
Projects. (On a container row `e` still opens a shell.) dpad finds the file from
the same `project.config_files` label the update uses; when the project has
several files, it asks which one. It then suspends the TUI and opens a copy of
the file in `$VISUAL`, else `$EDITOR`, else `vi` (`notepad` on Windows). Editors
that return straight away need their wait flag, for example
`EDITOR="code --wait"`.

When the editor closes, dpad checks the copy with `docker compose config`, with
the copy standing in for the original and the project's other files and env
files as they are. The original is not touched until you save:

- If Compose rejects the edit, its error is shown. `e` reopens the editor with
  your changes kept; `esc` twice discards them.
- If the edit is valid, dpad shows what it changes in the running project (for
  example `web: nginx:1.27 → nginx:1.28`, or services added and removed) and a
  diff of the file. `enter` saves and opens the update preview for the project,
  `s` only saves, `e` edits again and `esc` twice discards.

Saving writes a temporary file beside the original and renames it over it, with
the original's permissions, so a failed write leaves the original as it was. No
backup is kept; keep Compose files in version control. If the file changed on
disk while you were editing, dpad refuses to overwrite it.

Only files on this machine can be edited for now. For a project whose files are
on an SSH host, `e` says so.

## Images

The Images list loads at startup and reloads when the tab is opened or with
`ctrl+r`. The usage column counts containers by image ID, including stopped
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

## Events

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

Interrupted subscriptions retry with one-to-five-second backoff, retaining the
buffer and view settings. A `stream gap` record marks an interruption: events
missed during disconnection are not replayed. Filters also apply to these gap
records; clear the filter or search `stream gap` to see them. This is an in-memory
session timeline, not a persistent audit log or historical event fetch.

## Volumes

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

## Networks

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

## Prune

Press `X` to preview a prune. It opens on the kind that matches the tab
(Images, Volumes or Networks) and on stopped containers elsewhere; `tab` and
`shift+tab` move between kinds. Nothing is removed until you press `y`.

| Kind | Preview lists | `a` adds |
|---|---|---|
| containers | exited, created and dead containers | |
| images | untagged images no container uses | tagged images no container uses |
| volumes | anonymous volumes no container mounts | named volumes no container mounts |
| networks | user-defined networks no container uses | |
| build cache | build cache records not in use | |

"No container" includes stopped containers, so an image or volume that a
stopped container still references stays. The defaults match `docker container
prune`, `docker image prune`, `docker volume prune`, `docker network prune` and
`docker builder prune --all`; `a` matches `docker image prune --all` and
`docker volume prune --all`. Sizes are shown for images (including shared layers)
and build cache.

After `y`, the list is read again. If it changed, the new preview is shown and
needs another `y`. Otherwise `dp` removes exactly the previewed resources, one
request each and without forcing, so anything that became used in the meantime
fails with Docker's error instead of being removed. Containers, volumes and
networks are removed four at a time. Images go one at a time, newest first, and
each tag is removed in turn; untagged parent images go with them, as with
`docker image prune`. Container removal keeps anonymous volumes. Each row shows
`removed`, `failed` or `unknown` (the request was interrupted and may have
applied); select a failed row to read the error. `esc` during removal cancels the
rest.

Build cache is pruned in one request, because Docker has no call to remove a
single record. Rows the daemon kept are marked `kept`, and records created after
the preview that were also removed are counted in the summary along with the
space reclaimed.

## Stats

### Compose projects

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
`dp stats`, snapshots, and piped output keep their existing flat behavior.

### History sparklines

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
below 80 columns or 10 body rows. Standalone `dp stats`, snapshots, and piped
output omit history storage. History uses
about 6 KiB per tracked container plus map overhead, adds no Docker requests,
and retains the existing batched stats redraws; only the selected container's
chart is rendered.

### Network and disk rates

Live Stats shows network RX/TX and disk read/write throughput in bytes/sec.
Press `t` to toggle cumulative byte totals; `dp stats --totals` starts standalone
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

### Stream recovery

Live Stats retries failed startup and interrupted container streams automatically,
also with one-to-five-second backoff. Samples that stop advancing for five seconds
show `—` and lose their old rates/history; the summary counts available streams
and excludes unavailable values. One shared watchdog restarts streams with no
progress for ten seconds. Recovered streams establish fresh rate/history
baselines. This applies to both the Stats tab and standalone `dp stats`, where
`ctrl+r` retries a failed watcher immediately. Quitting cancels background work.

## Custom commands

Bind your own commands to keys in `~/.config/dpad/config.yaml`
(`$XDG_CONFIG_HOME/dpad/config.yaml` if set, `%AppData%\dpad\config.yaml` on
Windows, or any path in `DPAD_CONFIG`). The file is read when `dp` starts.

```yaml
customCommands:
  - name: top
    key: T
    command: docker top {{.ID}}
  - name: bash
    key: b
    attach: true
    command: docker exec -it {{.ID}} bash
  - name: compose down
    key: O
    scope: project
    confirm: true
    command: docker compose -p {{.Project}} down

# mouse: false   # leave the mouse to the terminal
# theme: matrix   # see docs/themes.md
```

| Field | Meaning |
|---|---|
| `name` | shown in help, confirmations and the status line |
| `key` | a key `dp` doesn't already use, such as `b`, `o`, `T` or `ctrl+o` |
| `scope` | `container` (default): a container row in Containers, Stats, Projects or Events. `project`: a project row in Projects |
| `command` | run by `/bin/sh -c` (PowerShell on Windows) |
| `attach` | `true` hands the terminal to the command, for interactive commands; `dp` comes back when it exits |
| `confirm` | `true` asks `y/N` first |

The command is a Go template. Container commands can use `{{.ID}}`,
`{{.Name}}`, `{{.Image}}`, `{{.Project}}`, `{{.Service}}` and `{{.WorkingDir}}`
(the Compose project directory); project commands can use `{{.Project}}` and
`{{.WorkingDir}}`. Values are inserted already quoted as single words, so don't
add quotes around them. A value `dp` doesn't know, such as the project of a
container outside Compose, is an empty word.

Commands run with the same daemon as the dashboard: `DOCKER_HOST` is set to its
endpoint, as for shells. Without `attach`, a command runs in the background and
its last line of output, or its error, appears in the status line; use `attach`
when you want to read all of it, for example `docker logs {{.ID}} | less`.
Context switching waits for background commands to finish.

Problems in the file, such as a key `dp` already uses, a duplicate key or a
template error, are shown in the status line at startup; the other commands
still load.

## Narrow terminals

Tables fit the terminal width instead of running off the right edge. Long
names, images and ports shrink with a `…`, and when that isn't enough the
least useful columns are hidden:

| Tab | Hidden first |
|---|---|
| Containers | container ID, then image |
| Images | image ID, then created |
| Stats | disk I/O, memory bar, PIDS, CPU bar, network, memory usage |

Widen the terminal and they come back. Volumes and Networks switch to a compact
column set below 100 columns, and Events below 110.

## Responsiveness

The TUI renders at up to 120 frames per second, so a keypress reaches the
terminal within about 8 ms; unchanged frames write nothing.
Over SSH, event updates batch at most four times per second and log updates at
most ten times per second; log paging moves immediately without animation.
Each keypress still makes a round trip over the SSH connection; for the
smoothest navigation, run `dp` locally against the remote daemon (see
[Remote daemons over SSH](#remote-daemons-over-ssh)).
SSH detection uses `SSH_CONNECTION`, `SSH_CLIENT`, or `SSH_TTY`. Table layouts
are reused while navigating and across refreshes whose visible cells and columns
are unchanged. Lists scroll when the selection leaves the visible page, keeping
the viewport stable when moving within it. Automatic container-list refreshes
coalesce Docker event bursts into at most one request per second; manual refresh
and subscription recovery remain immediate.

## Colors

Colors come from the active theme: press `C` to pick one, or set `theme:` in
`config.yaml`, `--theme` or `DPAD_THEME`. [Themes](themes.md) has the gallery
and the theme file format. Output is plain when piped, when `NO_COLOR` is set,
or with `--no-color`.

## Daemon connection

`dp` honors `DOCKER_HOST`, `DOCKER_CONTEXT`, and the current docker context
(`docker context use …`), so it talks to the same daemon as `docker`. That
includes colima, Docker Desktop, and remote contexts.

`DOCKER_HOST` takes precedence over context selection. When it is unset,
`DOCKER_CONTEXT` overrides the saved current context; `default` uses the default
daemon endpoint. A missing or invalid selected context returns an error instead
of connecting to the default daemon.

### Remote daemons over SSH

Run `dp` on your laptop and point it at a server's daemon:

```sh
DOCKER_HOST=ssh://me@server dp
# or, with the docker CLI installed locally:
docker context create home --docker host=ssh://me@server
docker context use home
dp
```

The TUI then draws locally, so moving through lists doesn't wait on a network
round trip per keypress; only data loads travel over SSH, in the background.
This is smoother than running `dp` inside an SSH session.

`dp` starts one `ssh` process that forwards a private local socket to the
server's `/var/run/docker.sock` by default. Your `~/.ssh/config`, agent, and
known hosts apply, and password or host-key prompts appear before the TUI opens.
Your user on the server needs access to the socket (typically through the
`docker` group for a rootful daemon). If the
connection drops, `dp` starts a new one without prompting, so key-based or
agent authentication is needed for automatic recovery.

For rootless Docker or another socket location, append the absolute remote
socket path to the SSH endpoint:

```sh
DOCKER_HOST=ssh://me@server/run/user/1000/docker.sock dp
# or save it in a Docker context:
docker context create rootless --docker host=ssh://me@server/run/user/1000/docker.sock
docker context use rootless
dp
```

Replace `1000` with the remote user's UID (`ssh me@server id -u`) and make sure
their rootless daemon is running. The SSH user needs access to that socket;
rootless Docker does not require membership in the `docker` group. The path is
preserved when the tunnel reconnects. Paths must name an absolute socket file;
use URL encoding for spaces, `#`, or `?`. Shell variables and `~` are not expanded
on the remote host; use the actual path. `${...}` and NUL/newline characters are
rejected because OpenSSH interprets environment expansions locally.

Passthrough commands and the `e` shell run the local `docker` CLI, which
handles `ssh://` endpoints itself; on macOS `brew install docker` is enough
(no Docker Desktop needed).

### Connection status

The TUI shows its bound context/endpoint and connection state below the tabs.
A single daemon ping runs every two seconds with a two-second timeout. Failed
probes retry with backoff from one to five seconds; recovery refreshes lists and
open details without resetting filters or selection. In list tabs, `ctrl+r` refreshes and
requests an immediate connection check. Changing the Docker CLI's current context
outside `dp` leaves this dashboard bound to its existing daemon.

### Switching contexts

Press `c` from a list tab to choose a saved Docker context. Use the usual navigation
keys or `/` to filter by context name or endpoint, then `enter` to switch; `esc`
cancels. The selection overrides `DOCKER_HOST` and `DOCKER_CONTEXT` for this `dp`
session without changing the Docker CLI's saved current context. The `default`
choice uses the platform's default Docker socket.

Switching cancels the old daemon's requests, stats and event subscriptions, closes
its connection, and opens fresh views on the selected daemon. The active tab is
kept, while filters, selections, cached resources, stats history, and the event
timeline reset. Finish pending container actions and close details/logs before
opening the picker. SSH authentication prompts appear between dashboard sessions.
If opening the selected context fails, the previous daemon reopens with fresh
views and an error message. An unreachable daemon shows the existing disconnected
state and retry behavior after selection.
