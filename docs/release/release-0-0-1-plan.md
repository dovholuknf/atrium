# Release 0.0.1: the plan

Item `m-new-release-0-0-1`, status READY. Nothing here has been run. Every step that leaves the machine is for clint, by hand.

## Decisions

Decided by @fabric on 2026-10-05, under clint's standing order that directors answer technical and product questions.
They are in the item file too.

| Question | Decision |
| --- | --- |
| Version string and tag | `0.0.1`, tag `v0.0.1`, on the claude/main sha the orchestrator names at release time. |
| Signing | Unsigned for 0.0.1. `checksums.txt` is published with the assets and the notes say the installers are unsigned. Signing is the later item `m-new-release-signing`. |
| Channels | GitHub Release (archives, deb, rpm, checksums), the scoop bucket, and the docs site. The brew tap and the one-line install stay in the far backlog. |
| Who tags | `scripts/cut-release.sh --execute` from clint's machine for the first release, then the workflow once it has worked. |

## What must be live and passing first

1. The deploy queue, item `m-new-deploy-queue`. Met: it is merged into `claude/main` and deployed. Before tagging, open
   `/_hub/deploy-queue` on the hub (or `?format=md`) and expect it to list nothing. A commit listed there is landed and
   not yet live.
2. A clean gate: `bash scripts/ci.sh` passes on the commit to be tagged, with `ATRIUM_LOCATION` and
   `ATRIUM_DEBUG_INPUTLAG` unset.
3. The release commit is on `main` on the remote, and the tree is clean. `cut-release.sh` refuses a dirty tree.
4. This branch merged, so the refreshed site sources are on that commit.

## Who runs each step

| Step | Who |
| --- | --- |
| Deploy hub and rooms, confirm `/_hub/deploy-queue` is empty | clint, with the director |
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
curl -s http://localhost:7778/_hub/deploy-queue      # on the hub, nothing listed
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

Refreshed on this branch against the changelog up to 2026-10-04. Added over two passes: growlers and remind me,
the deploy-ready line, the pulls tab, `atrium_git_url`, `atrium_git_clone`, `wake`, `kind`, launching on another room,
PR placement across rooms, allowed folders, forge access, change requests, the deploy queue and the CLI verbs that were
missing. It builds locally with no broken links and the gate-hook script passes. The build is at
`build.claude/docs-site` and was not published. To look at it before release:

```bash
cd website && ATRIUM_DOCS_BASE_URL=/atrium/ npx docusaurus build --out-dir ../build.claude/docs-site
npx docusaurus serve --dir ../build.claude/docs-site --port 3031   # http://localhost:3031/atrium/
```

## Gate result on this machine

`bash scripts/ci.sh` was run on 2026-10-04 at the merge of `claude/main` (c9d65765) with both variables unset. It
FAILED, so the gate is not green here and this machine cannot vouch for a tag. What failed:

- This machine's setup: git signs commits with `~/.ssh/id_ed25519_sign.pub`, which is not here, so every test that makes
  a commit fails (`TestPRWorktree*` in `internal/api`, and every `cut-release` check in "what the release refuses",
  exit 128). `TestInstallWritesThroughASymlink` needs symlink rights Windows denies here. `internal/daemon`,
  `internal/gitsync` and `internal/link` hit the 600 s package timeout.
- Not this machine: `gofmt` flags `cmd/ptyhost-spike/pipe_windows.go`. The board step finds `title=` tooltips in
  `index.html`, `changereq.js`, `hubrepos.js` and others that `scripts/title-allowlist.txt` does not cover. The skins
  step finds `--on-danger` set only by daylight, frost, linen and paper. These come from `claude/main` and need an
  owner before a tag.
- Passed: go vet, go build, the contrast check, the opencode plugin, shell and PowerShell parse.

Run the gate again on a machine with a signing key and Linux or macOS, where the first group does not apply.
The full log is `build.claude/ci.log`.
