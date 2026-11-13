# Running CI on your own server

`runner/` adapts [Expense Tracker's runner setup](https://github.com/spar/expense-tracker/tree/main/runner):
the same pinned `ghcr.io/actions/actions-runner:2.338.2` image, registration
entrypoint, three independent services, and saved registration volumes. This
image adds `build-essential` for Go's CGO race detector; `actions/setup-go`
installs each job's requested Go version.

The switch is the same **GitHub repository Actions variable, `CI_RUNNER`**:

| GitHub variable | Where jobs run |
|---|---|
| Unset or empty | GitHub-hosted Ubuntu and macOS tests, Ubuntu lint and vulnerability checks |
| `self-hosted` | Your registered Linux runners for tests, lint, and vulnerability checks |
| `dockernext` | Your runners with the default extra `dockernext` label |

This is a repository variable under **Settings → Secrets and variables →
Actions → Variables**, not a variable inside a runner's `.env`. GitHub needs it
before it can assign a job to a runner. No repository settings are changed by
the Compose stack itself.

Private mode tests the two existing Go versions on Linux. macOS tests are
restored when `CI_RUNNER` is removed; a Linux container cannot run macOS tests.
Jobs wait when all private runners are busy or offline. They do not spill over
to GitHub-hosted runners while the variable is set.

## Start two or three runners

You need a Linux server with Docker Engine, the Compose plugin, and outbound
HTTPS access to GitHub and Go's download/module services. No incoming ports are
needed. Unlike Expense Tracker's pipeline, this repository's CI uses fake Docker
engines and does not start service containers or build Docker images. These
runners therefore use normal container networking and do **not** mount the
server's Docker socket or need `DOCKER_GID`.

1. Check out this repository on your server and enter `runner/`:

   ```sh
   cd dockernext/runner
   cp .env.example .env
   chmod 600 .env
   ```

   Defaults use the `dockernext-runner` Compose project, `dockernext-1/2/3`
   registration names, independent state volumes, and work folders under
   `/opt/dockernext-actions-runner`. These paths are separate from Expense
   Tracker's `/opt/actions-runner`. Edit `.env` if needed.

2. Obtain a registration token and put it in `RUNNER_TOKEN` in `.env`:

   ```sh
   gh api -X POST repos/spar/dockernext/actions/runners/registration-token --jq .token
   ```

   Or use **Settings → Actions → Runners → New self-hosted runner** and copy the
   token after `--token`. One token can register all three runners within its
   one-hour lifetime. A personal access token is not stored on the runners.

3. Start the desired number:

   ```sh
   # Two runners:
   docker compose up -d --build runner-1 runner-2

   # Or all three:
   docker compose up -d --build

   docker compose logs -f
   ```

   Wait for `Listening for Jobs` and confirm the runners are Idle in GitHub's
   runner settings. If reducing an existing three-runner stack to two, also run
   `docker compose stop runner-3`; selecting two services does not stop a third
   that is already running.

4. Clear `RUNNER_TOKEN` in `.env` and recreate the services you started:

   ```sh
   docker compose up -d --force-recreate runner-1 runner-2
   # Include runner-3 here if you started all three.
   ```

   Registration is restored from each runner's state volume. Recreating removes
   the original token from the containers' configured environment as well; no
   new token is needed for registered runners. Starting an unregistered third
   runner later needs a fresh token if the first one has expired.

5. Route CI to your server using the repository variable:

   ```sh
   gh variable set CI_RUNNER --body self-hosted --repo spar/dockernext
   ```

   The next workflow run uses the private runners. Each takes one job at a time;
   extra jobs queue until a runner is free. To return to GitHub-hosted runners:

   ```sh
   gh variable delete CI_RUNNER --repo spar/dockernext
   ```

   Remove the variable before taking your server offline if you want new runs
   assigned to GitHub's runners. Already queued jobs retain their runner choice;
   cancel and rerun them after changing the setting if necessary.

## Caches, updates, and saved state

Each runner has its own work folder with persistent Go SDKs in `_tool`, Go build
and module caches in `_cache`, and a golangci-lint cache. Workflows use these
local caches in private mode instead of restoring/uploading GitHub Actions Go
caches. Hosted mode keeps the existing Actions caching behavior.

The job-completed hook, adapted from Expense Tracker, clears only that runner's
`_tool` and `_cache` directories when their combined size exceeds
`RUNNER_CACHE_LIMIT_GB` (default 10). It removes diagnostic logs older than a week
and never prunes Docker images, containers, networks, or volumes. Container
output is rotated at three files of 10 MB each. Checkouts are reused and cleaned
by `actions/checkout` before each job.

Runners can update themselves. Periodically refresh the pinned base image and
rebuild, then recreate the services you use after current jobs finish:

```sh
docker compose build --pull
docker compose up -d runner-1 runner-2
```

`docker compose down` retains registration volumes and host work folders.
`docker compose down -v` deletes registrations and requires new registration
tokens. If GitHub has removed a runner, remove its old registration in GitHub
and reset only that runner's state volume before registering again. Keep
repository URL, runner names, and work roots consistent with saved registration;
changing those settings requires re-registration.

Use these persistent runners for trusted workflows in this private repository.
Jobs retain files between runs and run under the same account as the runner;
keep fork pull-request workflow execution disabled as in Expense Tracker's
setup. You do not need to open ports or grant jobs access to your server's
Docker daemon.
