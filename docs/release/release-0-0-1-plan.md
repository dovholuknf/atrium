# Release 0.0.1: the plan

Item `m-new-release-0-0-1`. Nothing here has been run. Every step that leaves the machine is for clint, by hand.

## Decisions asked of clint

These are in the item file too. The commands below assume the first answer in each row.

| Question | Assumed |
| --- | --- |
| Version string and tag | `0.0.1`, tag `v0.0.1`. The site, `website/package.json` and `docusaurus.config.js` already say 0.0.1. `packaging.md` shows `v0.1.0` only as an example. |
| Signing | None. Windows and macOS installers go out unsigned and the notes say so. A Developer ID cert and a code-signing cert are a later decision. |
| Channels | GitHub Release (archives, deb, rpm, checksums), the scoop bucket, and the docs site. No brew tap yet, no Chocolatey, no Store. |
| Who tags | `scripts/cut-release.sh --execute` from this machine for the first release, then the workflow once it has worked. |

## What must be live and passing first

1. The deploy queue, item `m-new-deploy-queue`, branch `claude/m-new-deploy-queue`. It is not merged. Merge it into
   `claude/main`, then deploy the hub and each room so the queue shows nothing owed.
2. A clean gate: `bash scripts/ci.sh` passes on the commit to be tagged, with `ATRIUM_LOCATION` and
   `ATRIUM_DEBUG_INPUTLAG` unset.
3. The release commit is on `main` on the remote, and the tree is clean. `cut-release.sh` refuses a dirty tree.
4. This branch merged, so the refreshed site sources are on that commit.

## Who runs each step

| Step | Who |
| --- | --- |
| Merge the deploy queue, deploy hub and rooms | clint, with the director |
| Gate | clint |
| Dry run of the release script | clint |
| Tag, push, release upload | clint |
| Scoop bucket commit and push | clint |
| Docs site publish | the `docs` workflow on a published release, started by clint |
| Brew tap | not in 0.0.1 |

Agents run none of them.

## Commands, in order

Run from a clean checkout of the commit to release.

```bash
# 0. Be on the release commit and make sure the deploy queue is empty and the gate is green.
git fetch origin
git switch main && git pull --ff-only origin main
git status --short                      # must print nothing
env -u ATRIUM_LOCATION -u ATRIUM_DEBUG_INPUTLAG bash scripts/ci.sh

# 1. Cheap refusals only.
bash scripts/cut-release.sh v0.0.1 --preflight

# 2. Full dry run. Builds all platforms, checks the stamped version and the reproducible build,
#    writes build.claude/release/v0.0.1/scoop/atrium.json. Touches nothing remote.
bash scripts/cut-release.sh v0.0.1

# 3. Tag, push the tag and publish the release. Pushing the tag fires release.yml, which runs the same
#    script, so one of the two finds the release already there. Use this one the first time.
bash scripts/cut-release.sh v0.0.1 --execute

# 4. The scoop bucket, in the bucket repository and not this one.
cp build.claude/release/v0.0.1/scoop/atrium.json <bucket>/bucket/atrium.json
cd <bucket> && git add bucket/atrium.json && git commit -m "atrium 0.0.1" && git push

# 5. On any Windows machine, the real test of the release shape.
scoop bucket add dovholuknf https://github.com/dovholuknf/scoop-bucket
scoop install atrium
atrium version                          # v0.0.1

# 6. The docs site. Publishing the release in step 3 already triggers it. To run it by hand instead:
gh workflow run docs.yml --repo dovholuknf/atrium
```

If step 3 fails after the tag is pushed, do not move the tag. Fix forward with `v0.0.2`.

## The docs site

Refreshed on this branch against the changelog since the last refresh (2026-09-28). Added: growlers and remind me,
the deploy-ready line, the pulls tab, `atrium_git_url`, `atrium_git_clone`, `wake`, `kind`, launching on another room,
and PR placement across rooms. It builds locally with no broken links and the gate-hook script passes. The build is at
`build.claude/docs-site` and was not published. To look at it before release:

```bash
cd website && ATRIUM_DOCS_BASE_URL=/atrium/ npx docusaurus build --out-dir ../build.claude/docs-site
npx docusaurus serve --dir ../build.claude/docs-site --port 3031   # http://localhost:3031/atrium/
```

Not covered and left for a later refresh: change requests between rooms, allowed folders at provisioning, and the
forge access work, which are operator and fabric surfaces still moving.
