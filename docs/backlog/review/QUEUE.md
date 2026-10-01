# @review queue

**Review each landing in `notes/director-reports.md`, room-side shas first. End each review with a verdict line,
ROOM DEPLOY OK or HOLD, with the sha.**

This file is the queue. Take the next item from the top of "Waiting". After a restart or a new context, read this
file first, then the tail of `D:/git/github/dovholuknf/atrium/notes/director-reports.md` for landings not listed here
yet.

## How a review is done

1. Read the merge's diff against its first parent (`git diff <sha>^1 <sha>`), code before docs.
2. Run package-scoped tests only, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared. A whole package past Go's
   10 minute default is not a failure. Rerun the tests that cover the change with `-run` and `-timeout 25m`.
3. Probe live listeners read-only when the change is live: GETs and harmless requests, never exit, launch, approve,
   delete or patch a real card.
4. Write `docs/backlog/<dept>/<d>-new-review-<sha>.md`: what holds, findings severity-ranked with file:line evidence,
   tests, and the verdict line.
5. Commit on `claude/review-runtime-0930`, rebase on `claude/main`, fast-forward `claude/main` in the main checkout.
6. One line in `notes/director-reports.md`: time, @review, what, the verdict with the sha, the file, the review commit.
7. A HIGH goes to the owning director and to orchestrator-sg4-control@sg4-control at once, before the line.

## Workers go off sg4

Rule from clint, 2026-09-30. A review worker is launched on sg3 or m1mini, whichever has fewer running (count with
`atrium_peers rooms=true`), never sg4. `<room>` below is that one:

1. `pwsh -File scripts/room-git.ps1 worktree <room> claude/<id>` prints the remote worktree path, cut from
   `claude/main`.
2. `atrium_launch` with `room=<room>` and `cwd=` that path.
3. Its reports come home with `atrium_git_collect room=<room>`, as `<room>/claude/<id>`.

Each room's cap is 5, shared by all directors. If a launch fails, tell orchestrator@sg4-control and launch on sg4 in
the meantime.

## Waiting

- Board landings from @ui come FIRST (clint, via the orchestrator, 2026-09-30 19:25), read only and fast, ahead of
  everything below.
- r-deploy-ready f82abf78 is on HOLD (`r-new-review-f82abf78.md`): re-read the link room-side fix and the click's
  build-equals-tip and `ATRIUM_NEW_BUILD` fix. When it lands, re-stamp with `room-ok` every board landing since the
  installed build. Two rules from now on: a board review carries `hub-ok` AND `room-ok`, and an `internal/link`
  review carries `room-ok` too, since the room runs it. A re-read's OK range starts at the ORIGINAL base, so it covers
  the held patch.
- u-m-changes 7e496aa8 is on HOLD (`u-new-review-7e496aa8.md`): re-read the `quoteOne` control-character fix when
  @ui sends it. Owed lows, no hold: u-m-typefix pen `pointercancel` (`u-new-review-7b70705e.md`), and @runtime's
  defense in depth, dropping `\x1b[201~` inside any wrapped paste.
- From cc3bd954 on, @ui, @runtime and @fabric run Sonnet (orchestrator, 6h test): every review of theirs carries a
  "Quality:" line.
- M3 of `docs/rnd/machine-health-design.md` (the profiler agent's rules) before it ships, when it is built.

## Open lows, for when a fix names them

- `r-new-review-3c354ed4.md` 1: a websocket attach by name still guesses past a quiet room.
- `r-new-review-f16ff0c8.md` 1-3: ziti with no `ATRIUM_HOSTS` is open to rebinding, names fixed at start, a
  Host-rewriting access proxy.
- `r-new-review-a1abed4f.md` 1: the event insert maps every constraint, a `CHECK` included, to "no card".
- `u-new-review-5e68b83f.md` 2: quiet without the terminal attached is only the badge.
- `r-new-review-a92bb5f7.md`: 4 lows.
- `u-new-review-8333259b.md` L1 L2, parked in @ui's QUEUE.
- `u-new-review-eb94e016.md` 1.
- `r-new-review-e25a2c2f.md` 1-2: a wildcard over a public suffix or a dynamic DNS domain, an ignored entry is silent.
- `r-new-review-7065bd6f.md` 1: a hand-passed non-loopback `--board-addr` for `atrium rooms` is refused (on @runtime's
  queue).
- `u-new-review-8b316172.md` 1-2: phone question links against none on the desktop, a choice press can send twice.

## Done

2026-09-30: the security audit (16058425), ff747683, 0b4e3f0d, 5edc1821, a23a9034, 06b876bb, 54794900, 3c354ed4,
5e68b83f, f16ff0c8, b144c66a, c184ae8c, f4466ea0 (ROOM DEPLOY OK), befa812b and a1abed4f (ROOM DEPLOY OK).
Then f-026 e738577e, u-growler eb94e016, u-ready-spam 23fa4602 (closes the 5e68b83f medium), card URLs 8333259b and
5954f802, @runtime a92bb5f7 (R1 R3, ROOM DEPLOY OK, 1 medium open), the card-audit landing 53b3b570, wildcard hosts
e25a2c2f, the paste spinner pair 6ca9e81f 76b9f736 (ROOM DEPLOY OK) and the growler reply 8b316172.
