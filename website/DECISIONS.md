# Docs site decisions

The decisions behind `website/`, so the next person to touch it does not re-make them. Newest last.

## Docusaurus

**Kept.** The site is a docs site with a landing page in front, and Docusaurus is built for exactly that: a
sidebar generated from files, MDX for the pieces that need a component, tabs and admonitions, a light/dark switch,
a build that refuses broken links and anchors, and versioned docs for when a second release needs its own copy.
The alternatives either drop the docs half (a plain Astro or Vite site means building a sidebar, search and
versioning by hand) or drop the landing half (MkDocs Material is excellent at docs and fights a custom front page).

## No Tailwind: Infima variables plus CSS modules

**Chosen: Infima custom properties for the theme, CSS modules for the landing page.** Docusaurus ships Infima as
its CSS layer, and Tailwind's preflight resets the same elements Infima styles, so the two fight over every
heading, list and link in the docs. Tailwind scoped to the landing page with preflight off would work, and it
would mean a PostCSS plugin, a config file and a second styling vocabulary for one page. The palette is a dozen
tokens copied from the board, which is what CSS custom properties are for. `src/css/custom.css` holds them, and
each landing section has its own class in `src/pages/index.module.css`.

## The palette is atrium's own

The site and the product should read as one thing, so every colour comes from the board:

| Site | Source |
| --- | --- |
| Dark mode | The board's default skin, `internal/api/web/css/tokens.css`: navy `#0B1B2E`, teal `#00E3B0`, blue `#28C2FF`, the 60px grid |
| Light mode | The board's `daylight` skin, `internal/api/web/css/themes.css`: `#F8FAFC`, teal `#008496`, blue `#1D63C4` |
| Worker green | The `active-work` terminal theme, `internal/api/web/js/themes.js`: `#0f3d1a` on `#80ff90` |
| The A | `drawAtriumA` in `internal/api/web/js/core.js`, redrawn as `static/img/logo.svg` |

Dark is the default, and the site follows the system preference when it has one.

## Drawn mockups, not screenshots

The board on the landing page is drawn in HTML (`src/components/BoardMockup.js`, `Mockups.js`). It stays sharp at
any width, follows the light/dark switch, reflows on a phone, and never shows a real path, repository or session.
A screenshot would go stale with the next board change, and this site is refreshed only at release time.

## Refreshed at release, not per feature

The site describes atrium as of one release, `0.0.1` first. It is rebuilt when a release is cut, so it does not
chase every feature. The release number is `release` at the top of `docusaurus.config.js`.

At release time:

1. Bump `release` in `docusaurus.config.js` and `version` in `package.json`.
2. Read the CHANGELOG since the last release and update the pages it touches. Describe only what is on the
   release's commit.
3. `pwsh ./scripts/build-docs.ps1`, then look at it (below).
4. Publish the GitHub release. `.github/workflows/docs.yml` runs on `release: published` and deploys.

When a second release needs the first one's docs kept, `npx docusaurus docs:version 0.0.1` snapshots them.

## Hosting: GitHub Pages under /atrium/

The site is published at `https://dovholuknf.github.io/atrium/`. `baseUrl` is `/atrium/`, and every link and asset
must carry it. `scripts/build-docs.ps1` checks the built HTML for any root-relative link that does not, on top of
Docusaurus's own broken-link check.

The workflow holds no logic: it checks out, installs node, asks `configure-pages` for the origin and base path,
passes them to the script, and uploads and deploys the output. It runs on a published release and on
`workflow_dispatch`, never on push. The Pages source in the repository settings must be set to GitHub Actions.

## Kept out of the site

- Anything not on `claude/main` at the time of writing.
- Trial surfaces the repository keeps for comparison, parked work, and the older heartbeat federation.
- Private details from the history: no customer names, hostnames or ticket contents.

## Design review (codex, before first publish)

A codex session reviewed the site read-only. What changed because of it:

- **The gate is not promised by default.** The hero, the features and the intro say atrium gates tool calls, and
  the intro now carries a table of what works out of the box and what you add: the gate hook, and `atrium room`
  (then `atrium2`) for rooms. The quick start puts sessions on the board first and adds the gate as an optional step
  that ends in a request you can see and block.
- **An edited approval never runs the original.** The starter gate script refuses and hands the edited command
  to the agent. `website/scripts/test-gate-hook.js` runs that exact script, extracted from `docs/hooks.md`, against
  a mock of atrium's agent port on every docs build.
- **Install starts with the download.** Version-pinned release URLs, `mkdir -p ~/.local/bin`, PATH guidance, and a
  from-source path with its prerequisites. The packages carry `atrium` only, and the docs say so.
- **"No cloud" became "self-hosted, one operator".** "One machine" read wrong next to rooms, and "no cloud" could
  be read as the agents never reaching their model providers.
- **The drawn board is captioned as an illustration**, and the sidebar puts install and the quick start before the
  story.

Not taken, on purpose:

- **Restructuring the landing page into three outcomes.** Editorial, and the sections each earn their place. Worth
  another look at the next release.
- **Docs search.** Algolia needs an account and a crawler, and a local search plugin is another dependency for 22
  pages. Add it when the docs outgrow the sidebar.

## Guards

- `website/go.mod` makes `website/` its own Go module, so `go vet ./...` from the root never walks
  `website/node_modules`.
- `website/.gitignore` keeps `node_modules/`, `build/` and `.docusaurus/` out of git.

## Running it locally

```powershell
cd website
npm ci
npm start                      # live reload at http://localhost:3000/atrium/
npm run build                  # the static site, into website/build
npx docusaurus serve --port 3031   # the built site at http://localhost:3031/atrium/
```

Screenshots of the landing page, desktop and phone, light and dark:

```powershell
$env:NODE_PATH = '<a node_modules with playwright>'
node scripts/screenshots.js http://localhost:3031/atrium/
```
