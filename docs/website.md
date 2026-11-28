# Putting the website on dpad.sh

This guide connects Cloudflare to this repository so the website in
[`website/`](../website/) goes live at https://dpad.sh and updates itself each
time a change to it is merged into `main`. It takes about 15 minutes, all in
the browser. It follows the same setup as the Fastling website.

Cloudflare now sends new projects to **Workers** rather than Pages, so this
guide uses the Workers screens. For a folder of HTML like this one, a Worker
with static assets does the same job as Pages: no code runs, Cloudflare serves
the files.

Cloudflare docs: [Workers Builds configuration](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/),
[build watch paths](https://developers.cloudflare.com/workers/ci-cd/builds/build-watch-paths/),
[static assets](https://developers.cloudflare.com/workers/static-assets/),
[headers](https://developers.cloudflare.com/workers/static-assets/headers/),
[redirects](https://developers.cloudflare.com/workers/static-assets/redirects/)
and [custom domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/).

## What the website is

| Address | What it serves |
| --- | --- |
| https://dpad.sh | the home page, `website/public/index.html` |
| https://dpad.sh/install | a redirect to [`scripts/install.sh`](../scripts/install.sh) on `main` |
| https://dpad.sh/install.ps1 | a redirect to [`scripts/install.ps1`](../scripts/install.ps1) on `main` |
| https://dpad.sh/sitemap.xml, /robots.txt | the page list and crawler rules for search engines |
| any other address | `website/public/404.html`, with a 404 status |

[`website/wrangler.jsonc`](../website/wrangler.jsonc) tells Cloudflare what to
do: the Worker is called `dpad`, it serves `website/public/`, and unknown
addresses show `404.html`. `_headers` and `_redirects` in `website/public/`
add the security headers and the install redirects; Cloudflare reads them and
does not serve them.

Nothing in this setup runs in GitHub Actions or needs a GitHub secret.
Cloudflare's GitHub app reads the repository and builds on Cloudflare's side,
so CI, releases and the OpenSSF Scorecard checks are unchanged.

## What it costs

Nothing beyond the domain. Cloudflare's
[static assets billing page](https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/)
says requests to static assets are free and unlimited, and the site has no
Worker code. Builds are included in the free Workers plan.

## Before you start

- The pull request that adds `website/wrangler.jsonc` is merged into `main`.
  Without it, Cloudflare finds no settings file, tries to set the project up by
  itself and opens a pull request of its own. Close that pull request if it
  appears.
- You can sign in to the Cloudflare account that holds dpad.sh.

## Step 1: Connect the repository

1. Open the [Cloudflare dashboard](https://dash.cloudflare.com/) and choose
   **Workers & Pages** (it may be under **Compute**).
2. Select **Create application**, then **Import a repository** (or **Continue
   with GitHub**).
3. If Cloudflare's GitHub app is already installed for your account (from
   Fastling), open its settings on GitHub
   (**Settings**, **Applications**, **Cloudflare Workers and Pages**,
   **Configure**) and add `spar/dpad` under **Only select repositories**.
   Otherwise GitHub asks now; choose **Only select repositories** and pick
   `spar/dpad`.
4. Back in Cloudflare, select `spar/dpad`. You now see **Set up your
   application**.

## Step 2: Build settings

| Field | Value | Why |
| --- | --- | --- |
| Project name | `dpad` | Must match `"name"` in `website/wrangler.jsonc`, or the deploy fails. |
| Build command | leave empty | There is nothing to build. |
| Deploy command | `npx wrangler deploy` | The default. It uploads `website/public`. |
| Root directory (under **Advanced settings**; may be called **Path**) | `website` | Cloudflare runs the deploy inside `website/`, where `wrangler.jsonc` is. |

Select **Deploy**. When it succeeds, open the `workers.dev` address it shows
and check the home page, the light/dark switch, and a made up address such as
`/nope`, which should show "Page not found".

If the setup screen has no root directory field, deploy anyway, then open the
Worker's **Settings**, **Build**, set **Root directory** to `website` and
select **Retry build**.

## Step 3: Only rebuild when the website changes

Most commits are to dpad itself. In the `dpad` Worker, open **Settings**,
**Build**, **Build watch paths**, set **Include paths** to `website/*` and save.

Optional: tick **Enable Preview Builds** on the same page. A pull request that
changes `website/` then gets a preview address as a comment, and a
"Workers Builds: dpad" check. Only a merge into `main` changes dpad.sh. The
`"previews": {}` line in `wrangler.jsonc` is what preview builds need.

## Step 4: Use dpad.sh

1. In the `dpad` Worker, open **Settings**, **Domains & Routes**, **Add**,
   **Custom domain**.
2. Type `dpad.sh` and select **Add custom domain**. The domain is in the same
   account, so Cloudflare adds the DNS record and certificate itself. If it
   says a record already exists, delete the old `A`, `AAAA` or `CNAME` record
   for `dpad.sh` under the domain's **DNS**, **Records**, and try again.
3. Under the domain's **SSL/TLS**, **Edge Certificates**, check that **Always
   Use HTTPS** is on.
4. Open https://dpad.sh and check the install redirect:

   ```sh
   curl -sI https://dpad.sh/install | grep -i location
   curl -fsSL https://dpad.sh/install | head -3
   ```

   The first prints the `raw.githubusercontent.com` address of
   `scripts/install.sh`; the second prints the top of the script.

Once dpad.sh works you can turn off the `workers.dev` address under
**Domains & Routes**.

## Step 5: Protect the domain

`curl -fsSL https://dpad.sh/install | sh` runs whatever dpad.sh serves, so the
domain is now part of how people install dpad. The script itself checks each
download against the release's SHA-256 checksums, but whoever controls
dpad.sh controls the script.

- Turn on two-factor authentication for the Cloudflare account.
- Under the domain's **Registrar** settings (**Manage Domains**, dpad.sh), keep
  **Auto renew** on and the registrar lock enabled.
- Keep Cloudflare Web Analytics off. The site promises no tracking.

## Step 6: Google Search Console

Search Console shows how dpad.sh appears in Google and lets you ask Google to
crawl it. It adds nothing to the site, so the no-tracking promise holds.

1. Open [Google Search Console](https://search.google.com/search-console),
   choose **Add property**, then **Domain**, and type `dpad.sh`. A Domain
   property covers every address on dpad.sh.
2. Google shows a TXT record that starts with `google-site-verification=`.
   In Cloudflare, open the domain's **DNS**, **Records**, **Add record**: type
   `TXT`, name `@`, content the whole value Google gave. Save it. If Search
   Console offers to add the record in Cloudflare for you, that works too.
3. Back in Search Console, select **Verify**. If it fails, wait a few minutes
   and try again.
4. Open **Sitemaps**, type `sitemap.xml` and select **Submit**.
5. Open **URL inspection**, paste `https://dpad.sh/` and select **Request
   indexing**.

Pull request previews (`<branch>.dpad.sh`) and `workers.dev` addresses send
`X-Robots-Tag: noindex` from `_headers`, and every page names
`https://dpad.sh/` as its canonical address, so only the real site is indexed.
Keep the TXT record: Google checks it again from time to time.

## Changing the website later

- **Text or layout:** edit `website/pages/` (or `site.css`), run
  `make website`, open a pull request with a `docs(website):` title, check the
  preview, merge. [website/README.md](../website/README.md#pages) explains the
  page format.
- **New screenshots:** after `make screenshots`, run `make website` and commit
  `website/public/assets/` with the new images. `go test ./...` fails until
  you do.
- **Preview locally:** `python3 -m http.server -d website/public 8000`, then
  open http://localhost:8000.
