# Running dpad locally

This guide takes you from a fresh clone to running `dp` against a demo set of
containers.

- [1. Prerequisites](#1-prerequisites)
- [2. Build and run from source](#2-build-and-run-from-source)
- [3. Install `dp` on your PATH](#3-install-dp-on-your-path)
- [4. Start the demo environment](#4-start-the-demo-environment)
- [5. Development loop](#5-development-loop)
- [6. Troubleshooting](#6-troubleshooting)

## 1. Prerequisites

| Tool | Version | Check | Needed for |
|---|---|---|---|
| Go | 1.26.7+ | `go version` | building |
| Docker Engine | any recent | `docker info` | everything. Docker Desktop, colima, OrbStack and Linux all work |
| `docker` CLI | any | `docker version` | passthrough of commands `dp` doesn't enhance |
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

**On Linux (Ubuntu/Debian):**
```sh
# Install Go (adjust version as needed)
wget https://go.dev/dl/go1.26.7.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.7.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Or use your distro's package manager:
sudo apt-get update && sudo apt-get install -y golang-go

# Install Docker Engine
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER
newgrp docker

# Install Docker Compose v2 (if not included with Docker)
sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

# Install make (optional)
sudo apt-get install -y make
```

**On Linux (Fedora/RHEL/CentOS):**
```sh
# Install Go
sudo dnf install -y golang

# Install Docker Engine
sudo dnf -y install dnf-plugins-core
sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo
sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl start docker
sudo usermod -aG docker $USER
newgrp docker

# Install make (optional)
sudo dnf install -y make
```

**On Windows (with WSL2 or Git Bash):**

Using **WSL2** (recommended):
1. Install WSL2 with Ubuntu: `wsl --install -d Ubuntu`
2. Inside the WSL2 terminal, follow the **Linux (Ubuntu/Debian)** instructions above
3. Install Docker Desktop for Windows with WSL2 backend integration

Using **Git Bash** (Windows native):
```bash
# Install Go from https://go.dev/dl/ or use Chocolatey:
choco install golang

# Install Docker Desktop for Windows from https://www.docker.com/products/docker-desktop

# Install make for Windows (optional):
choco install make
```

Alternatively, use **Scoop** or **winget**:
```bash
# With Scoop
scoop install go
scoop install docker
scoop install make

# With winget
winget install GoLang.Go
winget install Docker.DockerDesktop
```

After installing, ensure `DOCKER_HOST` points to your Docker daemon (Docker Desktop handles this automatically).

## 2. Build and run from source

```sh
cd dpad
make build                 # → bin/dpad
./bin/dpad ps -a     # try it
./bin/dpad           # full-screen TUI (q to quit)
```

Without `make`:
```sh
go build -o bin/dpad .
```

To run straight from source without building, use `make run`:
```sh
make run ARGS="stats --sort mem"
go run . logs -f my-container      # same thing without make
```

`dp` uses whatever daemon `docker` uses (`DOCKER_HOST`, `DOCKER_CONTEXT`, or the
current `docker context`). As a quick sanity check, `./bin/dpad ps -a` should
list the same containers as `docker ps -a`.

To work against Docker on another machine (for example a home server), run `dp`
on your Mac with `DOCKER_HOST=ssh://me@server` instead of inside an SSH session.
For a rootless daemon, include its remote socket path, for example
`DOCKER_HOST=ssh://me@server/run/user/1000/docker.sock` (using the remote user's UID).
See [Remote daemons over SSH](usage.md#remote-daemons-over-ssh).

## 3. Install `dp` on your PATH

```sh
make install                 # → ~/.local/bin/dpad and ~/.local/bin/dp
```

If `~/.local/bin` isn't on your PATH yet, add it to your shell's rc file:

**bash:**
```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
dp --version
```

**zsh:**
```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
dp --version
```

**fish:**
```sh
fish_add_path ~/.local/bin
dp --version
```

**Other ways to install:**
- System-wide (no PATH changes needed): `sudo make install PREFIX=/usr/local`
- With `go install .`: this puts `dpad` in `$(go env GOPATH)/bin`. Add `alias dp=dpad` to your shell rc file.
- **Shell completion:** for example, `dp completion zsh > "${fpath[1]}/_dp"`. See `dp completion --help`.

To remove it, run `make uninstall` (use the same `PREFIX` you installed with).

## 4. Start the demo environment

`testenv/compose.yaml` starts about 20 containers, each designed to exercise one
dp feature. Among them are a CPU hog, a container near its memory limit, an
unhealthy one, a crash-looping one, a paused one, several log formats, and
network and disk I/O generators.

```sh
make demo-up        # pulls alpine, nginx, python (~165MB total) and starts everything
dp                  # explore. The demo containers are all named dp-demo-*
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
make lint      # golangci-lint, pinned in the Makefile; nothing to install
make vuln      # govulncheck
make build && ./bin/dpad stats
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
- **Colors and piping:** colors are disabled automatically when output is piped, so `dp ps | less` shows plain text. Run `dp ps` directly in a terminal to see colors.
- **Re-running the TUI:** after a change, `go run .` is the fastest way to try it.
- **Version string:** `make build` stamps it from `git describe`. Other builds (`go build`, `go install`) report the module version Go records, and `dev` if there is none.

## 6. Troubleshooting

| Symptom | Fix |
|---|---|
| `Cannot connect to the Docker daemon` | Start your engine (`colima start`, or open Docker Desktop). Check `docker context ls`: `dp` follows the context marked `*`. |
| `dp ps` shows different containers than `docker ps` | `DOCKER_HOST` set in one shell but not the other? `dp` honors `DOCKER_HOST` > `DOCKER_CONTEXT` > the current context. |
| No colors | Output is piped or redirected, `NO_COLOR` is set, or you passed `--no-color`. |
| A row shows `0.00%` CPU for a moment | The first sample of a newly started container has no previous sample to diff against. It fills in after about 1s. |
| `dp: docker CLI not found in PATH` | Passthrough needs the `docker` binary. Enhanced commands (`ps`, `images`, `stats`, `logs`, the TUI) work without it. |
| `docker: unknown command: docker compose` | Compose isn't installed as a plugin. See [Prerequisites](#1-prerequisites), or run `make demo-up COMPOSE=docker-compose`. |
| Demo containers fail with `/scripts/...: not found` | The engine can't see the repo directory. colima and Docker Desktop share your home directory by default, so clone under `~` or add the path to the engine's file-sharing settings. |
| TUI looks garbled | Use a terminal with UTF-8 and 256 colors (iTerm2, the macOS Terminal, kitty, WezTerm, Windows Terminal). Over tmux, set `TERM=tmux-256color`. |
| `ssh tunnel to …: Permission denied` | `dp` runs `ssh` with your `~/.ssh/config`. Check `ssh me@server docker ps` works first; your server user must be in the `docker` group. |
| `go: go.mod requires go >= 1.26.7` | Upgrade Go (`brew upgrade go`). |
