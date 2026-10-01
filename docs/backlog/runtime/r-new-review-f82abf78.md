# Review of r-deploy-ready f82abf78 at 3802baca (@runtime: hub orchestration (a), "deploy ready")

Reviewed by @review, 2026-10-01, from `git show f82abf78` and the tip 3802baca (two merges of claude/main, and a
docs-only rename to HS). Checked against my five rules in `docs/backlog/rnd/rd-new-review-ed68cd81.md`, and from the
reviewer's side: whether the verdicts I write today produce the right answer.

## What holds

The five rules:

1. **Patch-id matching.** `git patch-id --stable` over `log -p`, cached per SHA, on both sides. A change altered in
   a rebase gets no verdict, which is the safe answer.
2. **Range trailers.** `<base>..<tip>` resolved `--first-parent`, so the claude/main commits merged into a reviewed
   branch are not covered. A merge counts only through its `--remerge-diff`, so a clean merge needs nothing and a
   conflict resolution needs a verdict of its own.
3. **Newest verdict wins.** Verdicts are applied oldest first, and a conditional OK is an OK.
4. **The path rule** matches what I asked for: `.md` and images need nothing, `internal/`, `cmd/`, `scripts/`,
   `go.mod` and `go.sum` do, and `test-board-headless.js` is test-only.
5. **The ready line names** up to three blockers, with the reason for each.

Also holds:

- **A verdict sits only on a commit that touches review files and nothing else.** `land-review.ps1` commits exactly
  one review file, so every verdict I have written since ba008991 qualifies.
- **The installed commit is read from the binary file** (`version`), not from the running hub, and is cached on its
  path, mtime and size.
- **POST `/deploy` is `edge.LocalOperator` only.** It needs the full tip the board showed (a 409 if the branch has
  moved), re-checks readiness fresh, guards a double click under the lock, spawns detached, and is audited.
- **Nothing deploys on a timer.** The minute tick only re-reads, and only while a board is watching.
- gofmt is clean, `go vet` on deployready and link is clean, `go test ./internal/deployready/` is ok, and the link
  `Deploy|Ready` tests are ok.

## Findings

### Medium

1. **`internal/link` is not hub-only. The room runs it.** `hubOnly` lists `internal/link/` and `internal/hubstore/`,
   so a link change needs only a hub verdict. But the room process takes its join, dial, relay and keys from that
   package: `cli/roomrun.go`, `roomrelay.go` and `transport.go` use `link.Join`, `link.Dialer`, `link.Direct`,
   `link.RelayRequest`, `link.Keys`, `link.Zrok` and `link.Ziti`. A room restarts on the installed binary, so a link
   change with hub-ok only reaches the room unreviewed for the room. That is exactly what rule 1 of the design was
   meant to stop.
   - Fix: drop `internal/link/` from `hubOnly`, or split it by file if the hub-only files can be named (the docs API,
     deployready, the proxy). The safe default is room-side.
   - The cost falls on my side: reviews of link changes will carry room-ok, which most already do.
2. **The click can deploy something other than the tip it checked.** `deploy-ready.ps1` checks that claude/main
   equals `-Tip`, then runs `build-deploy.ps1`, which builds whatever `HEAD` is at that moment, and then
   `deploy-hub-only.ps1`, which installs `$AtriumNew`. Two gaps:
   - **A landing in between.** Directors land on claude/main every few minutes. If one lands after the check, the
     build is of a commit with no verdict. After the build, compare the built binary's `version` commit to `$Tip`,
     and refuse to install on a mismatch.
   - **`ATRIUM_NEW_BUILD` from the hub's environment.** `spawnDeployScript` passes `gitsync.CleanEnv()`, which strips
     only `GIT_*`. `live-common.ps1` takes `$AtriumNew` from `ATRIUM_NEW_BUILD` when it is set. `build-deploy.ps1`
     removes it only inside its own child process. So a hub started with that variable set installs the path it
     names, not the build just made. That is the stale-binary incident from 2026-09-29. Remove it in
     `deploy-ready.ps1` before calling `deploy-hub-only.ps1` (or set it to the built path), and strip `ATRIUM_*` in
     `spawnDeployScript`.

   The worker ran `-WhatIf` only, so neither gap was exercised.

### Low

3. **Board files need a room verdict, and mine carry hub-ok only.** `internal/api/web/**` is room-side under
   `RoomSide`, and that is correct: the room serves the board too, and a room restart serves whatever is installed.
   But every board verdict I have written is `hub-ok` alone, so every @ui landing would read blocked. This is my
   side to change, and no code change is needed. From now on, my board reviews carry `room-ok` as well, since a
   board review covers the files wherever they are served. Board landings already on claude/main since the installed
   build need a re-stamp, which I will do when this lands.
4. **A HOLD stays on the held patch until an OK covers that same patch.** That is right, but it means a re-read's OK
   range has to start at the original base, not at the fix commit. Otherwise the held commit, which is still in the
   branch, blocks forever. My re-reads have done this (1f4c8d34..7b8bed90, then 1f4c8d34..8a83bf61). Say it in the
   changelog contract so every reviewer does.
5. **GET `/_hub/deploy-ready` answers anyone past the share's password** with the checkout path and the script path.
   Other hub routes already disclose as much, so this is a note, not a change.

Quality: after the Sonnet switch. The five rules are implemented as written, with careful edges: remerge-diff for
merges, first-parent covers, the review-files-only rule, a fresh check and a tip match on the click. The misses are in
what the rules rest on: which package the room runs, and what the script installs as opposed to what it checked.
Neither is visible from the diff alone. No drop.

HOLD f82abf78~1..3802baca on findings 1 and 2. Lows 3 to 5 need no code from you, except the one line of contract in
low 4.
