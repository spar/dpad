# Demo environment

`compose.yaml` starts containers that each exercise a specific `dn` feature.
For setup, see [docs/local-development.md](../docs/local-development.md).

```sh
make demo-up     # from the repo root
make demo-down
```

All containers are named `dn-demo-<service>-1`. In the TUI, type `/dn-demo` to
show only these.

## What each container demonstrates

| Container | Shows in `dn` |
|---|---|
| `web` | **healthy** badge, multi-port compaction `8080→80/tcp, 8443→443/tcp`, nginx `[error]` lines on stderr (404s) |
| `traffic` | high **NET I/O**; drives `web` with page loads, a 5MB download and 404s |
| `cpu-hog` | about 90% CPU → **red** CPU cell and full bar (limited to 0.9 cores) |
| `cpu-warm` | about 60% CPU → **yellow** (0.6 cores) |
| `mem-hog` | about 87% of its 64MiB limit → **red** MEM cell |
| `mem-warm` | about 59% of 64MiB → **yellow** MEM cell |
| `disk-io` | growing **BLOCK I/O** (writes and reads 64MB every 3s) |
| `many-pids` | about 61 **PIDS** |
| `logger-text` | plain `INFO/WARN/ERROR/DEBUG` lines; WARN/ERROR go to **stderr** |
| `logger-logfmt` | `level=info` style (logfmt) |
| `logger-json` | `{"level": "error", ...}` style (JSON), plus a stderr line every 10 records |
| `logger-tty` | the same as `logger-text`, but with `tty: true` (a raw, non-multiplexed log stream) |
| `unhealthy` | **✗ unhealthy** badge |
| `health-starting` | **… starting** badge (stays in its start period for 1h) |
| `flaky` | **restarting** state (magenta); logs `FATAL` and exits 1 in a loop |
| `crashed` | **Exited (3)** → red |
| `completed` | **Exited (0)** → gray |
| `paused` | **paused** state (yellow). Paused by `make demo-up`. |
| `dn-demo-created` | **created** state (blue). Never started, made by `make demo-up`. |
| `<none>` image | a **dangling** image, dimmed in `dn images`. Made by `make demo-up`. |

## Feature tour

**Colored `ps`:**
```sh
dn ps -a                     # every state color plus health badges
dn ps -a --sort created -r   # oldest first
dn ps -q                     # unknown flag → passed through to docker
```

**Images:**
```sh
dn images                    # sizes, dangling image dimmed, total in the footer
dn images --sort name
```

**Live stats:**
```sh
dn stats                     # cpu-hog on top in red, cpu-warm yellow
#   press m → mem-hog (red), mem-warm (yellow) rise to the top
#   press n → traffic / web (network)
#   press i → disk-io (block I/O)
#   press p → many-pids
#   press r → reverse;  q → quit
dn stats --no-stream --sort mem    # one snapshot
```
Run `docker compose -f testenv/compose.yaml stop cpu-hog` in another
terminal and the row disappears. `start` it again and it comes back.

**Logs:**
```sh
# Merge several containers; each gets its own prefix color
dn logs -f dn-demo-logger-text-1 dn-demo-logger-json-1 dn-demo-logger-logfmt-1

dn logs --tail 50 dn-demo-web-1                          # nginx [error] lines highlighted
dn logs --tail 100 dn-demo-logger-text-1 --grep 'db|slow' # filter and highlight
dn logs -t --tail 20 dn-demo-logger-tty-1                 # TTY stream, with timestamps
dn logs dn-demo-flaky-1                                   # FATAL in red on every restart
```

**TUI:**
```sh
dn
```
1. Press `/`, type `dn-demo`, then `enter` to keep only demo containers.
2. Press `s` a few times to cycle the sort between name, created, and status.
3. Select `dn-demo-logger-json-1` and press `enter` to follow its logs. Press `esc` to go back.
   Press `d` to inspect its configuration, networks, and mounts. Use `ctrl+r` to
   refresh, `enter` to open logs from details, and `esc` to return.
4. Select `dn-demo-paused-1` and press `S` then `y`. It unpauses and turns green.
5. Select `dn-demo-cpu-hog-1` and press `x` then `y`. After about 10s it shows
   **Exited (137)** in red and drops off the Stats tab. The shell loop ignores
   SIGTERM, so docker has to kill it. Press `S` to bring it back.
6. Press `3` for the Stats tab, then `c`/`m`/`n`/`i`/`p` to re-sort live.
   Select `cpu-hog` or `mem-hog` to see two-minute CPU/memory history, averages,
   and peaks below the table. History fills as samples arrive; enlarge a small
   terminal to at least 80 columns to see the panel.
7. Press `2` for Images and `?` for help.
8. Press `4` for Projects. The `dn-demo` project shows service containers,
   running/total counts, and unhealthy containers. Press `space` on the project
   row to collapse/expand it, or `enter` for merged project logs. Try `/` with
   `(?i)error|warn`, then `enter`; use `ctrl+l` to clear and `esc` to return.
9. Select a running container such as `dn-demo-web-1` and press `e`, then `enter`
   to open `/bin/sh`. Run `pwd`, then `exit` to return to the dashboard. Try a
   stopped or paused container to see the preflight error; `esc` cancels the prompt.
10. Press `2` for Images and `d` to inspect an image's tags, metadata, and containers
    using it. Press `esc`, then `x` to read the removal confirmation and `n` to
    cancel. Images used by stopped containers are protected from deletion too.

**Passthrough:**
```sh
dn compose -f testenv/compose.yaml ps   # not enhanced → runs `docker compose ...`
dn network ls
```
