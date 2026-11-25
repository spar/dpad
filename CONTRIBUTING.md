# Contributing to dpad

Issues and pull requests are welcome. [docs/local-development.md](docs/local-development.md)
covers setting up Go and Docker on macOS, Linux and Windows, and
[AGENTS.md](AGENTS.md) describes the code layout and conventions.

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
   release version; see [AGENTS.md](AGENTS.md#releases).
4. If the change affects what the TUI looks like, start the demo environment
   (`make demo-up`) and regenerate the screenshots with `make screenshots`
   (see [scripts/screenshots](scripts/screenshots/README.md)).

## Adding a theme

A theme is one YAML file in [`themes/`](themes). Copy an existing one, set
every color, and `make test` checks the file for you, including that its text
stays readable on its background. The steps, the full list of color roles and
the accepted color values are in
[docs/themes.md](docs/themes.md#adding-a-theme-to-dpad).

Use a title like `feat(themes): add solarized-dark theme`, and include the
gallery screenshot from `make screenshots` if you can.
