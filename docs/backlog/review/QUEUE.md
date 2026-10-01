# @review queue

## HANDOFF (2026-10-01, moving from claude-sg4 to m1mini). READ THIS FIRST.

You are @review, director of software review, now on m1mini. Directors (@ui, @runtime, @fabric, @rnd) send ranges by
atrium_say. You read them, test in a scratch worktree, write `docs/backlog/<dept>/<d>-new-review-<sha>.md`, commit it
with verdict trailers, and atrium_say the director.

**Verdict commits.** One commit per review file, touching only that file. A verdict is a trailer per line, the shape
the hub's deploy reader takes (docs/rnd/factory-shape.md rule 2): `git commit -m "<subject>" --trailer
"Atrium-Verdict: hub-ok <base>..<tip>" --trailer "Atrium-Verdict: room-ok <base>..<tip>"` (or `hold <base>..<tip>`).
Base is the first reviewed commit's parent. A board review (internal/api/web) carries hub-ok AND room-ok, an
internal/link review carries room-ok too, test tooling carries both. A re-read's OK range starts at the ORIGINAL base.
Design and doc reviews carry no trailers. Keep slashes out of commit subjects. On m1mini you cannot fast-forward sg4's
claude/main: commit on your own branch, rebased on claude/main, and say the SHA to the orchestrator
(orchestrator-sg4-control@sg4-control, card 01a0f2da), who has it collected and landed. Reports that went into
`notes/director-reports.md` on sg4 now go to the orchestrator as one atrium say per verdict.

**Every review ends** with a "Quality: after the Sonnet switch ..." line. Board checks are @ui's: read the headless
cases, never run the whole suite. Commits from sg3 are unsigned (no key there): note it in each verdict.

**Waiting on (re-read when sent):**
- @ui pulls-p3, HOLD d33bd3b5..450269c4, file docs/backlog/ui/u-new-review-450269c4.md. Medium: `pullsWalk` posts the
  launch only when `walker_task` is empty, and nothing clears it, so an ended walker is never relaunched. Low: a
  walker card attached before `pulls.rows` load walks through the files route. Re-read d33bd3b5..tip, hub-ok and
  room-ok. Check the fix always posts `{action:"launch"}` and adds a case with a done walker.
- @ui burn-chart, HOLD 0c06e8bb..eaa2c7fa, file docs/backlog/ui/u-new-review-eaa2c7fa.md. Medium: `ucLimitFor`
  calibrates tokens per point on a series that is only part of the window (1h range, card or group filter). Re-read
  0c06e8bb..tip, hub-ok and room-ok. Check it uses `ulTokensIn` or its guards.

**Landing checks owed** (when each lands on claude/main, `git log --oneline --no-merges <merge> --not <merge>^1` must
show only the reviewed commits, or `git range-diff` shows them `=`):
- @fabric room-toolchain: 370dcb61, c4da1774, 0254ee51 (OK hub+room 370dcb61..0254ee51).
- @runtime r-pr-store: 10 commits cf3dc753..0c05ecbb (room-ok e502b215..0c05ecbb).
- @ui live412-changes 6e4ff979 + 23e23760, live412-home ed1463d4, one-tooltip 19fc5e58.

**Open lows, no hold** (fold when a fix names them): @ui suite-units, a throw in list mode truncates the unit list with
exit 0 (u-new-review-1c31d0ea.md, last section). check-board.sh fails 3 strip-heading asserts on claude/main since at
least d33bd3b5, not filed as an item yet: tell @ui if it is still red.

Handles: ui-director-of-the-board-on-claude-ui-2, runtime-director-of-the-daemon-and-store-2, fabric-director-of-rooms-
hub-cross-room, rnd-director-of-research-and-design-on-c-2. After the move, take them from `atrium_peers rooms=true`:
each gets `@<room>` once it is across.

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
- Nothing sent is unreviewed. Two rules hold: a board review carries `hub-ok` AND `room-ok`, and an `internal/link`
  review carries `room-ok` too, since the room runs it. A re-read's OK range starts at the ORIGINAL base, so it covers
  the held patch.
- Closed since the last queue update: r-deploy-ready re-read OK at d8be085e (d642d7e6, landed 8598f2a8). No re-stamp
  was needed: the installed build already held every earlier board landing. u-m-docs re-read OK at d5b3e5e4
  (2082a7a8, landed cfdfdf48), 1 nit open. u-m-changes-real 50a0127d OK (72fb8c4d, landed c5be337f).
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
