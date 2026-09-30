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

## Waiting

- @ui's cross-window fix for the medium in `docs/backlog/ui/u-new-review-5e68b83f.md`.
- @runtime's R4.
- @runtime's held-message escalation stages (`docs/rnd/held-message-escalation-design.md`).

## Open lows, for when a fix names them

- `r-new-review-3c354ed4.md` 1: a websocket attach by name still guesses past a quiet room.
- `r-new-review-f16ff0c8.md` 1-3: ziti with no `ATRIUM_HOSTS` is open to rebinding, names fixed at start, a
  Host-rewriting access proxy.
- `r-new-review-f4466ea0.md` 1-3: `guard` halts on other foreign key failures, `bootResumes` key, `testDaemon`
  should refuse unsaved harnesses.
- `u-new-review-5e68b83f.md` 2: quiet without the terminal attached is only the badge.

## Done

2026-09-30: the security audit (16058425), ff747683, 0b4e3f0d, 5edc1821, a23a9034, 06b876bb, 54794900, 3c354ed4,
5e68b83f, f16ff0c8, b144c66a, c184ae8c, f4466ea0 (ROOM DEPLOY OK).
