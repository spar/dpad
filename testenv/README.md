# Demo environment

Three Compose projects that look like a real machine, where each container
also exercises a specific `dp` feature. For setup, see
[docs/local-development.md](../docs/local-development.md).

```sh
make demo-up     # from the repo root
make demo-down
```

| Project | File | Containers |
|---|---|---|
| `bookshop` | `bookshop/compose.yaml` | `bookshop-<service>-1` |
| `monitoring` | `monitoring/compose.yaml` | named after the service (`container_name`), as many self-hosted setups do |
| `homelab` | `homelab/compose.yaml` | named after the service |

The images are real (Postgres, Redis, nginx, Prometheus, Grafana, Jellyfin,
Vaultwarden and so on), so the icons, image names and sizes look as they would
on your own machine. A few of them run a script from `scripts/` instead of
their usual program, to produce a load or a state on demand.

## What each container demonstrates

| Container | Shows in `dp` |
|---|---|
| `bookshop-web-1` | **healthy** badge, multi-port compaction `8080→80/tcp, 8443→443/tcp`, nginx `[error]` lines on stderr (404s) |
| `bookshop-loadgen-1` | high **NET I/O**; drives `web` with page loads, a 5MB download and 404s |
| `bookshop-api-1` | plain `INFO/WARN/ERROR/DEBUG` lines, WARN/ERROR on **stderr**; about 61 **PIDS** |
| `bookshop-billing-1` | `{"level": "error", ...}` style (JSON) logs, plus a stderr line every 10 records; about 87% of its 64MiB limit → **red** MEM cell |
| `bookshop-worker-1` | `level=info` style (logfmt) logs; about 60% CPU → **yellow** (0.6 cores) and 59% of 64MiB → **yellow** MEM cell |
| `bookshop-db-1`, `bookshop-redis-1` | Postgres and Redis with **healthy** badges |
| `bookshop-mailer-1` | **restarting** state (magenta); logs `FATAL` and exits 1 in a loop |
| `bookshop-migrate-1` | **Exited (0)** → gray |
| `prometheus`, `node-exporter` | real services at rest |
| `grafana` | `tty: true` (a raw, non-multiplexed log stream), with Grafana's own colored logs |
| `alertmanager` | **✗ unhealthy** badge (its health check asks the wrong port) |
| `jellyfin` | about 90% CPU → **red** CPU cell and full bar (limited to 0.9 cores); **… starting** badge (stays in its start period for 1h) |
| `vaultwarden` | a real service turning **healthy** |
| `uptime-kuma` | **paused** state (yellow). Paused by `make demo-up`. |
| `backup` | growing **BLOCK I/O** (writes and reads 64MB every 3s) |
| `ddns` | **Exited (3)** → red |
| `api-canary` | **created** state (blue). Never started, made by `make demo-up`. |
| `<none>` image | a **dangling** image, dimmed in `dp images`. Made by `make demo-up`. |

## Feature tour

**Colored `ps`:**
```sh
dp ps -a                     # every state color plus health badges
dp ps -a --sort created -r   # oldest first
dp ps -q                     # unknown flag → passed through to docker
```

**Images:**
```sh
dp images                    # sizes, dangling image dimmed, total in the footer
dp images --sort name
```

**Live stats:**
```sh
dp stats                     # jellyfin on top in red, bookshop-worker-1 yellow
#   press m → bookshop-billing-1 (red), bookshop-worker-1 (yellow) rise to the top
#   press n → bookshop-loadgen-1 / bookshop-web-1 (network)
#   press i → backup (block I/O)
#   live network/disk values are bytes/sec; press t to switch to cumulative totals
#   press p → bookshop-api-1
#   press r → reverse;  q → quit
dp stats --no-stream --sort mem    # one snapshot
```
Run `docker stop jellyfin` in another terminal and the row disappears.
`docker start jellyfin` brings it back.

**Logs:**
```sh
# Merge several containers; each gets its own prefix color
dp logs -f api billing worker

dp logs --tail 50 bookshop-web-1                  # nginx [error] lines highlighted
dp logs --tail 100 bookshop-api-1 --grep 'db|slow' # filter and highlight
dp logs -t --tail 20 grafana                      # TTY stream, with timestamps
dp logs bookshop-mailer-1                         # FATAL in red on every restart
```

**TUI:**
```sh
dp
```
1. Press `/`, type `bookshop`, then `enter` to keep only the bookshop's containers.
   The CPU column graphs `bookshop-worker-1` near the middle of the scale and
   the others near idle. Press `o`, move to **CPU graph** and press `enter` to
   try bars, meter and off styles; **Icons** switches the icons before
   container names; `esc` closes settings.
2. Press `s` a few times to cycle the sort between name, created, and status.
3. Select `bookshop-billing-1` and press `enter` to follow its logs. Press `esc` to go back.
   Press `d` to inspect its configuration, networks, and mounts. Use `ctrl+r` to
   refresh, `enter` to open logs from details, and `esc` to return.
4. Select `uptime-kuma` and press `S` then `y`. It unpauses and turns green.
5. Select `jellyfin` and press `x` then `y`. After about 10s it shows
   **Exited (137)** in red and drops off the Stats tab. The shell loop ignores
   SIGTERM, so docker has to kill it. Press `S` to bring it back.
6. Press `3` for the Stats tab. Compose projects start collapsed; select `bookshop`
   and press `space` or `enter` to see its service/container rows. `v` toggles the
   flat view, and `/bookshop` or a service name filters the project. Press
   `c`/`m`/`n`/`i`/`p` to re-sort live.
   Network/disk sorts follow the current rates/totals mode; press `t` to toggle.
   Select `jellyfin` or `billing` to see two-minute CPU/memory history, averages,
   and peaks below the table. History fills as samples arrive; enlarge a small
   terminal to at least 80 columns to see the panel.
7. Press `2` for Images and `?` for help.
8. Press `4` for Projects. Each project shows its containers, running/total
   counts, and unhealthy containers. Press `space` on a project row to
   collapse/expand it, or `enter` for merged project logs. Try `/` with
   `(?i)error|warn`, then `enter`; use `ctrl+l` to clear and `esc` to return.
   Keep the bookshop's logs open while running
   `docker compose -f testenv/bookshop/compose.yaml up -d --scale api=2` in another
   terminal: new sources join automatically. Try `restart api` or
   `up -d --force-recreate api` with the same Compose file; the viewer
   follows replacement sources and retains its filter and buffer. Scale back to
   one when finished. On the project row, `e` opens the project's `compose.yaml` in
   your editor; change an image tag and close the editor to see the check, the
   summary of changes and the diff. `esc` twice discards the edit.
9. Select a running container such as `bookshop-web-1` and press `e`, then `enter`
   to open `/bin/sh`. Run `pwd`, then `exit` to return to the dashboard. Try a
   stopped or paused container to see the preflight error; `esc` cancels the prompt.
10. Press `2` for Images and `d` to inspect an image's tags, metadata, and containers
    using it. Press `esc`, then `x` to read the removal confirmation and `n` to
    cancel. Images used by stopped containers are protected from deletion too.
11. Press `5` for Events to see lifecycle and health changes recorded since opening
    the TUI. Filter with `/bookshop`, or search `die`, `unhealthy`, or a service name.
    Restart a demo container in another terminal and watch the new events arrive.
    Navigate to keep an older event selected, `g` to follow newest arrivals again,
    or `enter` to inspect that event's container. The latest 1,000 records are kept.
12. Press `6` for Volumes. Filter by a volume, Compose project, or referencing
    container name; `enter` shows metadata and each container's mount destinations.
    Running and stopped references both count. `ctrl+r` refreshes and `esc` returns
    to the list. This tab is read-only and does not remove or prune volumes.
13. Press `7` for Networks and filter by `homelab` or a container name. `enter`
    opens network configuration; `tab` shows its endpoint IP/MAC addresses, and
    `enter` on an endpoint opens container details. Use `esc` to return through
    both panes. `ctrl+r` refreshes the network snapshot and shared container data.

**Passthrough:**
```sh
dp compose -f testenv/bookshop/compose.yaml ps   # not enhanced → runs `docker compose ...`
dp network ls
```
