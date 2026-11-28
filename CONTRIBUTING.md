# Contributing to dpad

Thanks for your interest in dpad. Bug reports, ideas and pull requests are
welcome. To keep dpad focused, every change is agreed in an issue before work
starts, and the maintainer ([@spar](https://github.com/spar)) reviews and
merges every pull request.

[docs/local-development.md](docs/local-development.md) covers setting up Go and
Docker on macOS, Linux and Windows, and [AGENTS.md](AGENTS.md) describes the
code layout and conventions.

Everyone taking part is expected to follow the
[Code of Conduct](CODE_OF_CONDUCT.md). Report problems to
[info@dpad.sh](mailto:info@dpad.sh).

## Issue first

1. **Open an issue** with the Bug report or Feature request form. New issues
   are labelled `needs-triage`.
2. **Wait for `approved`.** The maintainer either labels the issue `approved`
   or explains why it doesn't fit and closes it. Please don't start a pull
   request before then, so your time isn't spent on something that can't be
   merged.
3. **Say you'd like to take it.** Comment on the approved issue and you'll be
   assigned, so two people don't build the same thing. Issues labelled
   `help wanted` or `good first issue` are good places to start.
4. **Open a pull request** that says `Closes #<issue>` in its description. A
   check fails pull requests that don't link an approved issue.

Typo and broken-link fixes don't need an issue; the maintainer labels those
pull requests `trivial`. Security problems go through private reporting, not
issues (see [SECURITY.md](SECURITY.md)).

### What fits dpad

dpad is a faster, friendlier front end to the Docker CLI and a terminal UI for
it. Changes that make existing views and commands better, fix bugs or add
themes are the easiest to approve. Features that add large dependencies, new
external services, telemetry, or a lot of new configuration need a strong case
in the issue first.

## Review and merging

- Every pull request needs the maintainer's approval, all CI checks passing and
  all review conversations resolved. Only the maintainer merges.
- Pull requests are squash-merged, so the pull request title becomes the
  commit message.
- Pushing new commits after an approval needs a fresh approval.
- CI on a first-time contributor's pull request starts once the maintainer
  approves the run.

## Contributor License Agreement

The first time you open a pull request, a bot asks you to sign the
[Contributor License Agreement](docs/CLA.md) by posting a comment. You keep the
copyright in your work. The agreement lets the maintainer keep distributing
dpad, including under a different license in the future, without having to
track down every contributor.

## Pull requests

1. Keep each pull request to one change, with tests next to the code it
   touches (`*_test.go`). Tests use fakes rather than a live Docker daemon, so
   they run anywhere.
2. Run the same checks as CI before pushing:

   ```bash
   make test vet lint
   ```

3. Use a [Conventional Commits](https://www.conventionalcommits.org/) title,
   such as `fix(stats): ...` or `feat(tui): ...`. The type decides the next
   release version (see [AGENTS.md](AGENTS.md#releases)), and a check fails
   titles that don't follow the format.
4. If the change affects what the TUI looks like, start the demo environment
   (`make demo-up`) and regenerate the screenshots with `make screenshots`
   (see [scripts/screenshots](scripts/screenshots/README.md)).

## Adding a theme

A theme is one YAML file in [`themes/`](themes). Copy an existing one, set
every color, and `make test` checks the file for you, including that its text
stays readable on its background. The steps, the full list of color roles and
the accepted color values are in
[docs/themes.md](docs/themes.md#adding-a-theme-to-dpad).

A new theme still needs an approved issue first. Use a title like
`feat(themes): add solarized-dark theme`, and include the gallery screenshot
from `make screenshots` if you can.
