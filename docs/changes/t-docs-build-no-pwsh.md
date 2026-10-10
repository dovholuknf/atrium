# t-docs-build-no-pwsh: the docs build runs under bash

`scripts/build-docs.ps1` is ported to `scripts/build-docs.sh` and deleted. Behavior is the same: `npm ci`, the gate
script check, `docusaurus build`, the link check of the built HTML, and `.nojekyll`. The parameters Url, BaseUrl and
OutDir are now `--url`, `--base-url` and `--out-dir`. The script runs with `set -euo pipefail`.

Calls made without asking:

- The changelog entry is filed under `runtime`, since the item has no prefix that names a department.
- The script exports `MSYS2_ENV_CONV_EXCL` for the two `ATRIUM_DOCS_*` variables. Without it git-bash turns
  `/atrium/` into a Windows path on its way to node, and the link check fails on every page.
- `website/scripts/test-gate-hook.js` still spawns `pwsh`, because the gate script in `docs/hooks.md` is PowerShell.
  The GitHub runner has pwsh, so the workflow is fine. A machine with only Windows PowerShell needs a `pwsh` on PATH
  to get past that step. Left alone, it is a separate piece of work.
- `CHANGELOG.md` and `docs/rnd/doc-overhaul-plan.md` still name the old script, as history.

Run locally in git-bash on Windows: yes, with node 24. This machine has no pwsh, so a copy of powershell.exe named
pwsh.exe was put on PATH for the gate step. It ended with `==> checking every link carries /atrium/` and
`built: .../website/build`, exit 0.

## Test plan

## @LETTER@. The docs build runs under bash

### @LETTER@1. Default build

1. In bash (Linux or git-bash), run `bash scripts/build-docs.sh`.

**Expected:** it ends with `built: <repo>/website/build`, exit 0, and `website/build/.nojekyll` exists.

### @LETTER@2. Flags and the link check

1. Run `bash scripts/build-docs.sh --url https://example.com --base-url docs --out-dir /tmp/site`.
2. Open `/tmp/site/index.html`.

**Expected:** the build passes, and every root-relative `href` and `src` starts with `/docs/`.

### @LETTER@3. The workflow

1. Run the docs workflow by hand.

**Expected:** the build step runs the script with bash and the Pages deploy succeeds.
