# dpad.sh website

The website at https://dpad.sh. Cloudflare (a Worker with static assets, set up
by `wrangler.jsonc`) serves the `public/` folder as it is, with no build step.
Setup steps: [docs/website.md](../docs/website.md).

| Path | What it is |
| --- | --- |
| `public/index.html` | the home page |
| `public/404.html` | shown for any address that does not exist |
| `public/_headers` | security and cache headers Cloudflare adds; not served |
| `public/_redirects` | `/install` and `/install.ps1` redirect to the scripts in `scripts/`; not served |
| `public/assets/site.css`, `site.js` | styles, and the dark/light switch, install tabs and copy buttons |
| `public/assets/fonts/` | JetBrains Mono 400 and 700, Latin subset (SIL Open Font License) |
| `public/assets/shots/`, `shots-light/`, `themes/`, `logo.svg`, `og-image.png` | written by `make website`; do not edit by hand |
| `public/assets/apple-touch-icon.png` | the logo at 180 px |
| `sync/` | the Go program behind `make website`, and the test that fails when the copies are stale |

## Rules

- No analytics, cookies or files from other servers. The pages set a
  Content-Security-Policy in a meta tag that allows only this site's own
  files. It is a meta tag rather than a header so it doesn't apply to the SVG
  screenshots, which carry their own styles and fonts.
- Dark is the default. Light mode uses the colors of the built-in `paper`
  theme; the tokens are at the top of `site.css`. The visitor's choice is kept
  in their browser's local storage only.
- The page works without JavaScript: it shows dark mode and the Homebrew
  command, and hides the switch, tabs and copy buttons.
- Feature text comes from the README. Keep keys and claims in step with it.

## Screenshots

`make website` copies the SVG screenshots and theme previews from
`docs/images` and writes a light copy of each screenshot to `shots-light/`,
recolored with the paper theme's palette. Run it after `make screenshots` and
commit the result; `go test ./...` fails while the copies are stale.

## Previewing

```sh
python3 -m http.server -d website/public 8000
```

Then open http://localhost:8000. The redirects and headers only apply on
Cloudflare; a pull request preview build shows them.
