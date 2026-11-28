# dpad.sh website

The website at https://dpad.sh. Cloudflare (a Worker with static assets, set up
by `wrangler.jsonc`) serves the `public/` folder as it is, with no build step.
Setup steps: [docs/website.md](../docs/website.md).

| Path | What it is |
| --- | --- |
| `pages/` | the source of every page: `index.html`, `404.html`, `docs/`, `compare/` and `alternatives/`, plus `_layout.html`, the shared `<head>`, header and footer, and `_boot.js`, the few lines inlined in the `<head>` that apply a saved light choice before the page paints |
| `public/index.html`, `public/404.html`, `public/docs/`, `public/compare/`, `public/alternatives/`, `public/sitemap.xml` | written by `make website` from `pages/`; do not edit by hand |
| `public/_headers` | security and cache headers Cloudflare adds; not served |
| `public/_redirects` | `/install` and `/install.ps1` redirect to the scripts in `scripts/`; not served |
| `public/assets/site.css`, `site.js` | styles, and the dark/light switch, install tabs, copy buttons and theme preview dialog |
| `public/assets/fonts/` | JetBrains Mono 400 and 700, Latin subset (SIL Open Font License) |
| `public/assets/shots/`, `shots-light/`, `themes/`, `logo.svg`, `og-image.png`, `cli.svg` | written by `make website` from `docs/images`; do not edit by hand |
| `public/assets/apple-touch-icon.png` | the logo at 180 px |
| `sync/` | the Go program behind `make website`, and the tests that fail when the copies are stale, a page links to an address that doesn't exist, or a guide is missing from its overview |

## Rules

- No analytics, cookies or files from other servers. The pages set a
  Content-Security-Policy in a meta tag that allows only this site's own
  files. It is a meta tag rather than a header so it doesn't apply to the SVG
  screenshots, which carry their own styles and fonts.
- Every asset address in the pages carries `?v=` and a hash of all the assets,
  so `_headers` can let browsers cache `/assets/` for a year. `make website`
  adds it; a changed asset needs `make website` before it goes live. Fonts
  are fetched without `?v=`, so a changed font needs a new file name.
- `site.js` loads deferred, so it doesn't hold up the first paint.
- Dark is the default. The visitor's choice is kept in their browser's local
  storage only. The tokens for each mode are at the top of `site.css`.
- Light mode has three looks. `ink` (the default) is black on white like
  dpad's ink theme, `paper` uses the paper theme's colors, and `white` puts
  paper's colors on a white page. See [Changing the light look](#changing-the-light-look).
- The page works without JavaScript: it shows dark mode and the Homebrew
  command, and hides the switch, tabs and copy buttons.
- On screens wider than 1700px the page is scaled up with CSS `zoom` (the
  steps are at the end of `site.css`), so it fills large and 4K displays.
- Feature text comes from the README. Keep keys and claims in step with it.

## Pages

Each file in `pages/` starts with a block of fields, then the page's HTML:

```html
<!--
title: Monitor Docker containers on a remote server over SSH
heading: Monitor Docker on a remote server over SSH
nav: Remote Docker over SSH
description: One or two sentences for search results, at most 160 characters.
updated: 2026-11-28
order: 2
related: /docs/contexts/, /docs/edit-compose-file/
-->
<p class="lead">...</p>
```

| Field | Meaning |
| --- | --- |
| `title` | the `<title>`, followed by ` · dpad`; at most 70 characters with it |
| `heading` | the page's `h1`, if it differs from the title |
| `nav` | its name in the sidebar and breadcrumb, if it differs from the heading |
| `description` | the meta and Open Graph description |
| `og` | a different Open Graph description, if wanted |
| `updated` | the date the content last changed; it becomes the page's `lastmod` in the sitemap |
| `order` | its place in the sidebar |
| `related` | up to three pages listed at the end |
| `layout` | `bare` for pages that draw their own `<main>` (the home page and 404) |
| `noindex` | `true` keeps it out of search engines and the sitemap |

`pages/docs/remote-docker-ssh.html` is served at `/docs/remote-docker-ssh/`.
A new guide goes in `pages/docs/`, a link to it goes on `pages/docs/index.html`,
then `make website`. Comparison pages link every claim about another tool to
that tool's own docs and say the date it was checked; recheck them monthly and
when the other tools release.

## Screenshots

`make website` copies the SVG screenshots and theme previews from
`docs/images` and writes a light copy of each screenshot to `shots-light/`.
For `ink` these are the captures in `docs/images/light/`, which
`make screenshots` records with the ink theme. For `paper` and `white` they are
the dark screenshots recolored with the paper theme's palette. Run it after
`make screenshots` and commit the result; `go test ./...` fails while the
copies are stale.

## Changing the light look

1. Set `lightShots` in `sync/main.go` to `ink`, `paper` or `white`. It picks
   the light screenshots and the `data-light` attribute on every page.
2. Run `make website` and commit `public/`.

## Previewing

```sh
python3 -m http.server -d website/public 8000
```

Then open http://localhost:8000. The redirects and headers only apply on
Cloudflare; a pull request preview build shows them.
