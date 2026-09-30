# @fabric queue

Top down. @fabric takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @fabric reads this file first, then `HANDOFF.fabric.md` in its
worktree if there is one.

Each item: what it is, where the spec is, and its state. @fabric moves an item to "Done" with its landing sha when it
is live.

## Queue

1. **f-026: a room with no certificate cannot take a proven room's name.** `docs/backlog/fabric/f-026.md`. Built as
   the f-026 commit on claude/fabric, with hubstore migration `0006_room_enrolled` (approved). With @review. Then land, report,
   and a hub-only deploy.
2. **49: the hub half of atrium:everywhere.** `docs/rnd/everywhere-card-design.md`, `docs/backlog/fabric/49.md`.
   Large. A hubstore migration (it would be 0007) needs the orchestrator's OK. Reuse worktree
   `D:/worktrees/claude/atrium/49`, merging claude/main into it first. Its first run died.
3. **f-003 stage 1.** The `atrium_resources` tool (ctlclass.go, the worker set goes 6 to 7), `resources.md` by the
   state dir, `atrium resources init`, and one framing line on every card.
4. **The Linux autostart proof on cdzrok.** Check that the machine is up first.
5. **f-004 stage 1.** The narrow hub op that carries a `claude/<branch>` from room to room, plus
   `scripts/move-card.ps1`. Prove it on sg3 and m1mini.
6. **Later:** 46 stage 2 (the add-a-machine dialog), the f-025 hubstore notify test gaps, and 75's shared folder.

## Done

- 2026-09-30, the launch job, three steps, all landed at ffa8b319:
  1. sg3 and m1mini updated to a claude/main build (b8edf0e2), both attached with `git:true`.
  2. f-019 stage 1 live on both rooms: the hub syncs claude/main into each clone, and a room's branches are collected
     into the main checkout as `<room>/claude/*`.
  3. How a director places a worker on sg3 or m1mini: `docs/backlog/fabric/remote-workers.md`.
- 2026-09-30, a refused provision smoke launch prints the room's reason: e4eb68dd.
