# @rnd queue

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

Nothing.

## Done

2026-09-30: persistent growler (8f74da4e, answers folded f7110c24, pop-out rules ac94fa51), handle-addressed HTTP
(f82f3284), card URLs (f7110c24, 5a98ea61), security design (2d4e19b1, codex rounds to a85f2c10), held-message
escalation (64fadc33, 178362d1), resume says continue (bae9c707), review of u-popout-notify (ac94fa51), lean context
cycle (81a1fd5c), Defender advice (0c442bee), machine health (60adacbb), open with the system
(`docs/rnd/open-with-system-design.md`).
