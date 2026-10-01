# @rnd queue

## Handoff, 2026-10-01 18:40: @rnd moved from claude-sg4 to m1mini

clint, via the orchestrator: every director moves to m1mini. You are the successor on m1mini, worktree
`/Users/claude/git/github/dovholuknf/atrium-worktrees/rnd`, branch `claude/rnd`.

**Reporting from m1mini.** `notes/director-reports.md` lives on sg4 and is not reachable here. Report to the
orchestrator with `atrium_say` (kind fyi) to `orchestrator-sg4-control@sg4-control` (card 01a0f2da). Never `atrium_say`
to `atrium-87300`. Workers you launch stay on m1mini (sg3 fallback), never claude-sg4. Every commit goes to @review
(alias `review`) before landing, even doc-only, except this queue file.

**PAUSE (clint via the orchestrator, 2026-10-01):** start no new item and launch no worker. Finish in-flight work through @review and land it, then sit idle. Nothing of ours is in flight: @runtime took r-pr-run back. @rnd is idle. Do not send P3 or start the E2E.

**Landing from m1mini (orchestrator, 2026-10-01, HOLD lifted):** all m1mini worktrees share one clone, so worker branches read locally. To land, merge onto `claude/landing` in the m1mini clone, branched from `claude/main` and NEVER onto `claude/main` there. The hub collects it into sg4, and the orchestrator fast-forwards sg4's claude/main. Tell the orchestrator the claude/landing tip and the @review verdict. @rnd is card 01a0f8bd-af15 on m1mini, and @ui is local too (alias `ui`). pulls-p3 (sg3) is PARKED on HOLD c0bccc01 until the pause ends.

**In flight: the pulls view.** clint: "pull request review flow sucks ... not token efficient nor fast." Design
`docs/rnd/pulls-view-design.md`, API contract `docs/rnd/pulls-api.md`. clint ordered all of it built and tested, and
@rnd OWNS DELIVERY: send each stage to its owner in order, track it, one fyi line to the orchestrator per stage
landed. Done only when the E2E in design section 9 passes: a real PR (not a 378 replay) comes in through a door,
runs the recipe, and is walked in the pulls view on the LIVE board. Record in `review.json` and the row: open to
walk-ready wall time, cost, fork cache reads, finding count, and whether a human or director was waited on (must be
none). Pass: under 10 minutes and under $2, and clint's read of the walk order. Reopen the stage that misses.

| stage | owner | state |
| --- | --- | --- |
| P1a r-pr-render | @runtime | LANDED 58461561 |
| P1b r-pr-store | @runtime | LANDED 59d21773 (@review c5c759f5). Needs a room deploy, the orchestrator's |
| P1c r-pr-run | @runtime (m1mini card 01a0f8cc) drives it again: fix, @review, claude/landing, 378 acceptance | @review HOLD on dbb76109 (HIGH: forks load the PR's .claude settings and hooks; MEDIUM: a retry after merge renders an empty review as ready). r-pr-run is fixing it and reports to @runtime, which copies @rnd |
| P2 pulls view | @ui | LANDED b6edcfd6 (@review c9284027) |
| P2 follow-up | @ui | IN FLIGHT: PUT findings/{key}, walk.js re-point, unmock drawer findings, walk marks, walker, start. @ui tells @rnd at landing |
| Hub | @fabric | LANDED d77d21d0, lows 370dcb61. Needs a hub deploy |
| Rules 6/10 | @review | DONE a3fc844f |
| P3 doors + 2nd opinion | @runtime, @ui, @review (gwt) | NOT SENT. Send when P1c lands. Design section 9 P3 |
| E2E | @rnd | last. Needs P3, hub and room deploys. Run off sg4 |

On each landing: `git merge-base --is-ancestor <sha> claude/main` before calling it landed.

**Done today, nothing to build:** Telegram notify design 5abf6282, $2,500 room machine research 404f6def (three open
questions for clint in that doc). The 378 measurements are in section 1 of the pulls design. Do not redo them.

**Open nits:** none of ours.

**Design reviews: every director design before it is built.**

This file is the queue. Take the next item from the top of "Waiting". After a restart or a new context, read this
file first, then the tail of `D:/git/github/dovholuknf/atrium/notes/director-reports.md` for designs not listed here
yet, and `D:/git/github/dovholuknf/atrium/notes/rnd-queue-2026-09-30.md` (or that day's file) for questions parked for
clint.

## How a design is done

1. Read the backlog item, and the code it touches, before writing. Check whether it is already built.
2. Write `docs/rnd/<name>-design.md`: the answer first, what is there today, stages with owner, size and an acceptance
   test each, and questions for later. Commit a copy of the backlog item with it when it is untracked.
3. Commit on `claude/rnd`, rebase on `claude/main`, fast-forward `claude/main` in the main checkout.
4. Park questions only clint can answer in the day's rnd queue file under "Questions for later". Never wait on him.
5. One line in `notes/director-reports.md`: time, @rnd, what landed, the sha. Then `atrium_report` to the launcher.

## Where a worker runs

Off sg4 (clint, 2026-09-30), on whichever of `sg3` or `m1mini` has fewer running (check with `atrium_peers`). Each
room's cap is 5, shared by every director. `<room>` below is the one chosen.

1. `pwsh -File scripts/room-git.ps1 worktree <room> claude/<id>` prints the remote worktree path, cut from
   `claude/main`.
2. `atrium_launch` with `room=<room>` and `cwd=<that path>`.
3. The work comes home with `atrium_git_collect room=<room>`, as `<room>/claude/<id>`.

If a launch there fails, tell orchestrator-sg4-control@sg4-control, and launch on sg4 meanwhile.

## How a design review is done

Read the director's design against the code and against the designs already on file. File findings for the owning
director as `docs/backlog/<dept>/<d>-new-<name>.md`, or fold them into the design they touch. One report line.

## Waiting

HELD by the PAUSE: do not start until the orchestrator ends it. Take them in this order:

1. `docs/backlog/rnd/rnd-new-room-handoff.md`: move a card and its work between rooms (`atrium move`), from the 2026-10-01 director move.
2. `docs/backlog/rnd/rnd-new-room-to-room-access.md`: a SPIKE on how one room reaches another's machine (direct ssh, a zrok private share, or OpenZiti), opt-in per room pair. Its output is a spike doc and a recommendation.
3. `docs/backlog/rnd/rnd-new-factory-landscape.md`: a SPIKE on other software factories, open source first. Its output is a landscape doc and a ranked list of what to borrow.
4. `docs/backlog/rnd/rnd-new-backlog-in-atrium.md`: a SPIKE on a pluggable backlog (files, atrium's store, GitHub, GitLab or Bitbucket issues). The output is a short spike doc with a recommendation, and nothing gets built. Do NOT start it until clint says so, even after the pause ends.

All four were filed by the orchestrator from clint on 2026-10-01.

## Done

2026-09-30: persistent growler (8f74da4e, answers folded f7110c24, pop-out rules ac94fa51), handle-addressed HTTP
(f82f3284), card URLs (f7110c24, 5a98ea61), security design (2d4e19b1, codex rounds to a85f2c10), held-message
escalation (64fadc33, 178362d1), resume says continue (bae9c707), review of u-popout-notify (ac94fa51), lean context
cycle (81a1fd5c), Defender advice (0c442bee), machine health (60adacbb), open with the system
(f0e0d97e), reply suggestions (`docs/rnd/reply-suggestions-design.md`), local proxy trust (e0b6c860), factory shape
and hub orchestration (`docs/rnd/factory-shape.md`, clint via the orchestrator, design only), Web Push for the phone
(`docs/rnd/web-push-design.md`, same), hub documents (`docs/rnd/hub-documents-design.md`, same).
