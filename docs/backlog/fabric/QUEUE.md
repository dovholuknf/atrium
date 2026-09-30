# @fabric queue

Top down. @fabric takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @fabric reads this file first, then `HANDOFF.fabric.md` in its
worktree if there is one.

Each item: what it is, where the spec is, and its state. @fabric moves an item to "Done" with its landing sha when it
is live.

## Queue

0. **f-new-defender-at-provision: built and landed, waiting on an administrator.**
   `docs/backlog/fabric/f-new-defender-at-provision.md`. `scripts/room-defender.ps1`, and a `defender` step in
   `provision-room.ps1`. sg3 has GOTMPDIR set. Its exclusions need an administrator on sg3 to run
   `C:\Users\claude\.atrium\provision\defender-exclusions.ps1` elevated, and sg4's file is at the same path. Close
   this when both have been run: `Get-MpPreference` in the elevated shell lists the five paths.
1. **f-026's four review lows** (`docs/backlog/fabric/f-new-review-e738577e.md`). Two are fixed in the landing
   commit and two need no code. Open until the deploy has been checked:
   - Low 1, deploy note: claude-sg4, m1mini and sg3 have no `joined` line left, so they are unenrolled until their
     first certificate attach after the migration. The hub-only deploy restarts the hub and every room reattaches,
     which marks them. After the deploy, check `enrolled_at` is set for all four rooms in the hub store. Until then a
     legacy dial under one of their names is accepted while that room is detached.
   - Low 2, no change: `Spend` marks the name enrolled before the certificate is signed. A spent secret needs a new
     join string anyway, so a failed signing leaves nothing to exploit.
   - Low 3, fixed: `TestOldPathCannotTakeAnAttachedProvenName` covers `upgradeKind` and `gitKind` too.
   - Low 4, fixed: `TestAKeylessAttachCannotReplaceAKeyedRoom` tests the `Hub.control` keyless-over-keyed refusal by
     itself.
2. **49: the hub half of atrium:everywhere.** `docs/rnd/everywhere-card-design.md`, `docs/backlog/fabric/49.md`.
   Large. A hubstore migration (it would be 0007) needs the orchestrator's OK. Reuse worktree
   `D:/worktrees/claude/atrium/49`, merging claude/main into it first. Its first run died.
3. **f-003 stage 1.** The `atrium_resources` tool (ctlclass.go, the worker set goes 6 to 7), `resources.md` by the
   state dir, `atrium resources init`, and one framing line on every card.
4. **The Linux autostart proof on cdzrok.** Check that the machine is up first.
5. **f-004 stage 1.** The narrow hub op that carries a `claude/<branch>` from room to room, plus
   `scripts/move-card.ps1`. Prove it on sg3 and m1mini.
6. **Later:** 46 stage 2 (the add-a-machine dialog), the f-025 hubstore notify test gaps, and 75's shared folder.
   Also small: `room-toolchain.ps1 local` calls a bare `powershell.exe`, which is not on the PATH of sg4's agents.
   `room-defender.ps1` uses the System32 path for the same call.

## Done

- 2026-09-30, the launch job, three steps, all landed at ffa8b319:
  1. sg3 and m1mini updated to a claude/main build (b8edf0e2), both attached with `git:true`.
  2. f-019 stage 1 live on both rooms: the hub syncs claude/main into each clone, and a room's branches are collected
     into the main checkout as `<room>/claude/*`.
  3. How a director places a worker on sg3 or m1mini: `docs/backlog/fabric/remote-workers.md`.
- 2026-09-30, a refused provision smoke launch prints the room's reason: e4eb68dd.
- 2026-09-30, f-026, a room with no certificate cannot take or relay as a proven name: passed by @review
  (e738577e), landed with lows 3 and 4 fixed. The landing sha is in `notes/director-reports.md`.
