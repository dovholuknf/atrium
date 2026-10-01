# Review of pulls-p3 450269c4: the walk drawer reads and writes /v1/prs findings

Reviewer: @review, 2026-10-01. Range d33bd3b5..450269c4 on sg3/claude/pulls-p3, one commit, unsigned (sg3 has no key),
noted. Read against the real routes in internal/api/prsdrawer.go (`getPRFindings`, `putPRFinding`, `walkPRFinding`,
`walkerPR`, `launchWalker`). Board checks are @ui's: I read the pullsDrawer and pulls cases and did not run the suite.

**Verdict: HOLD d33bd3b5..450269c4** on the medium below.

## The contract, checked

- `GET findings`: the drawer reads `file`, `text`, `hash`, `key`, `position`, `sev`, `path`, `line`, `link`, `leak`
  (a string, so `!!f.leak` is right), `hunk` and `walk`, all of which `PRFinding` carries. `hash` is of the raw bytes and
  `text` is LF-folded, and the PUT compares against the raw bytes, so a save quoting the read hash passes.
- `PUT findings/{key}`: body `{text, hash, eol}` matches `textIn`. The answer's `key` is taken, and `dock.cur` follows
  it, so an edit that moves the key keeps its place. The 409 carries `text` and `hash` at the top level (`prError`
  merges `extra`), which is where `e.body.text` and `e.body.hash` read them. `keep mine` quotes the disk hash, which is
  the raw hash the server compares.
- `POST findings/{key}/walk`: `done|skipped|deferred|open` and `url` only for done, as the server enforces. The
  answer's `walk` and `counts` are taken, and the row repaints.
- `POST walker`: `{action: "launch"}` answers 201 with `task`, or 200 with the live walker and `launched: false`.

## MEDIUM: a walker that has ended can never be launched again from the board

`pullsWalk` posts the launch only when `row.walker_task` is empty, and otherwise attaches that card. Nothing ever
clears `walker_task` when the walker's card ends: the only writers are `SetPRWalker` from `launchWalker` and the
explicit `set` and `clear` actions (internal/api/prsdrawer.go:506, :512, :560). So once a walker finishes or dies, the
`walk` button attaches a done or dead card, and no button posts a launch.

The server already has the answer: `launchWalker` (prsdrawer.go:531-535) hands back the live walker with
`launched: false`, and launches a new one only when the old card is done or dead. The old board posted the launch on
every click and so got this for free. Fix: always post `{action: "launch"}`, and attach `out.task`. That is one round
trip, and it makes the server the one that decides whether a walker is still alive.

Not covered: no headless case has a row whose `walker_task` names a done card. Add one: the click must post the launch
and attach the new task.

## LOW: a walker card attached before the pulls rows arrive walks through the files route

`walkTenant.probe` asks `walkPrOf`, which needs `pulls.rows`. Those load on the stream's open (`onPullsStreamOpen`) or
when the pulls tab opens. A walker card attached first (a reload with the card open, a click in the term list) has a
worktree that IS the run folder, which holds `findings/`, so the files probe says yes, the drawer opens with `prId`
empty, and the marks are written as `Walk:` lines into the finding files. The daemon reads walk state only from
walk.txt (`readWalk`), so those marks never reach the row's counts, and the next walk through the pulls drawer does not
see them. `pullsChanged` re-probes only when `!dock.tenant`, so the drawer stays in files mode once rows arrive. Fix:
also re-probe when `dock.tenant === walkTenant && !walkTenant.prId && walkPrOf(termTask.id)`. Reasoned from the code,
not run.

## NITS

- `walkPrItem` parses with `eol` `"\n"`, so the first save of a CRLF finding file rewrites it with LF. Harmless for a
  finding. Say so, or carry the eol in the GET.
- `u` on an open finding still says "this finding is neither posted nor skipped". With defer it is three states.

## check-board at the base

@ui asked me to confirm the 3 strip-heading FAILs fail at the base. Confirmed: `bash scripts/check-board.sh` in a
scratch worktree at d33bd3b5 fails the same 3 (single member org, a row under its own headings, a pathless session
under the catch-all), exit 1, and at 450269c4 it fails the same 3 and nothing else (check-board-450269c4.ps1 beside
land-review.ps1). Not this change's. It is a pre-existing break in the strip check and wants its own item.

Verdict: HOLD d33bd3b5..450269c4. Re-read d33bd3b5..tip, hub-ok and room-ok.

Quality: after the Sonnet switch, the contract is followed field by field, including the raw versus folded hash. The
miss is a lifecycle edge: the row's walker was treated as current forever.
