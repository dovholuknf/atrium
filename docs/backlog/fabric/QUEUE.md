# @fabric queue

Top down. @fabric takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @fabric reads this file first, then `HANDOFF.fabric.md` in its
worktree if there is one.

**Everything goes through @review before claude/main, scripts included** (orchestrator, 2026-09-30, after 53ccc3b4
landed unreviewed with a HIGH). Land only what @review has passed, at the sha it passed.

Each item: what it is, where the spec is, and its state. @fabric moves an item to "Done" with its landing sha when it
is live.

## Handoff, 2026-10-01 ~19:00, the move from claude-sg4 to m1mini

@fabric moves to m1mini. Read `HANDOFF.fabric.md` in the worktree for the long version. In short:

- **In flight:** nothing. Everything of mine has landed, the last being the burn-chart merge 6953184a (relay for @ui).
- **Pause holds** (clint, 2026-10-01): no new item and no new worker until the orchestrator lifts it.
- **Landing from m1mini:** merge onto `claude/landing` in the m1mini clone, branched from claude/main, never onto
  claude/main there. The hub collects claude/landing into sg4 and the orchestrator fast-forwards sg4's claude/main to
  it. Tell the orchestrator the claude/landing tip and the @review verdict. Land only the sha @review passed.
- **Relay duty is over:** m1mini worktrees share one clone, so a director reads any m1mini branch locally. A branch
  that lives on sg3 is not readable from m1mini, so such work parks until the git route is settled.
- **Waiting on clint:** Defender exclusions on sg3 and sg4 (item 0), `hooks.claude` on m1mini and sg3, auto-login for
  user `claude` on m1mini.
- **Waiting on @ui:** the `'rooms'` TypeError in groupDrag, growlActions, peekEverywhere, termDebug and the
  context-size page. Then I rerun `node scripts/test-board-sharded.js --save-weights` (runs local, needs NODE_PATH at
  a worktree with node_modules) and send only `scripts/board-suite-weights.json` through @review.
- **Open nits:** @review's D1 nits (cap the isText back-up loop at 3 steps in hubstore, a docOrigin comment above
  docBy in internal/link/docs_api.go, `/m/docs/` trailing slash 404), the line over 120 chars in room-defender (0b),
  and board-suite-remote args that cannot hold a space.
- **Owed by the orchestrator:** a hub deploy for d77d21d0, 0963c4c6 (pulls view) and 370dcb61 (pulls 404 text).
- **Reports** go to the orchestrator as an atrium say (card 01a0f2da on sg4-control). `notes\director-reports.md` on
  sg4 is not reachable from m1mini.
- **Hooks:** scripts, not typed git, for anything the branch guard refuses. The guard reads the shell's starting
  directory, so a detached HEAD there blocks `rebase`, `add` and `switch` for later calls.

## Queue

0z. **D1, hub documents, the hub half** (orchestrator, 2026-09-30 ~22:00: clint approved building it, exempt from the
   UI-only order). Spec `docs/rnd/hub-documents-design.md` (@review OK 1f142d27). Contract for @ui's D2 is
   `docs/backlog/fabric/hub-documents-api.md`. Worker `d1` (card 01a0f57c on m1mini, branch `claude/d1`), four stages.
   Migration is `0007_docs` in hubstore, @runtime reviews it. Then @review, then land, then a hub deploy. First use
   once live: publish `notes\USAGE-2026-09-29-30.md` and the factory evals so clint can read them on the phone.
0. **f-new-defender-at-provision: landed, 5bfbb1a2 (@review passed it as 08779d34), waiting on clint.** sg3 has
   GOTMPDIR set. Its exclusions need the line in `notes/director-reports.md` (18:53) pasted into an elevated shell on
   sg3. sg4 the same, with the line `room-defender.ps1 local` prints. Close this when `Get-MpPreference` in that shell
   lists the paths.
0b. **room-defender nit** from @review's pass of 7d1625cc: the header line ending "So GOCACHE=C:\ excludes nothing."
   runs past 120 characters. Rewrap it with the next room-defender change, through @review. (The low itself, Go paths
   held to AppData\Local and ~\go, landed as 8b6afd3c.)
0c. **live ATRIUM_HOSTS, @review's two lows on 8df971e4** (`f-new-review-8df971e4.md`). (1) Start-Room still
   inherits the caller's ATRIUM_HOSTS, and whether a room gets the User value depends on whether it starts after
   Start-Hub set it in the script's process. Give Start-Room the same read. (2) A value set only at Machine scope is
   now ignored: fall back to Machine when User is unset. Small, through @review. @runtime's r-new-hosts-setting
   replaces all of it later.
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
2. **Small, after the UI-only order lifts.** (a) @review's nit on 7725078d: a test pinning link's `humanLauncher` to
   `store.HumanLauncher`. (b) `TestAPatientAskMadeWhilePausedWaitsForResume` (internal/link/restartgate_test.go:316,
   "after the resume the ask said busy") fails about 1 run in 10 with `-count=10`, at e18b3c04, before 49 landed. A
   flake in the hub's restart gate. (c) The full daemon suite failed once at 345s on the 49 gate and passed on the
   rerun. The failing test was not captured.
3. **f-003 stage 1.** The `atrium_resources` tool (ctlclass.go, the worker set goes 6 to 7), `resources.md` by the
   state dir, `atrium resources init`, and one framing line on every card.
4. **The Linux autostart proof on cdzrok.** Check that the machine is up first.
5. **f-004 stage 1.** The narrow hub op that carries a `claude/<branch>` from room to room, plus
   `scripts/move-card.ps1`. Prove it on sg3 and m1mini.
6. **Later:** 46 stage 2 (the add-a-machine dialog), the f-025 hubstore notify test gaps, and 75's shared folder.
   Also small: `room-toolchain.ps1 local` calls a bare `powershell.exe`, which is not on the PATH of sg4's agents.
   `room-defender.ps1` uses the System32 path for the same call.

## Done

- 2026-09-30, 49, atrium:everywhere (a card that answers its bare name from every room): landed as e4136207,
  passed by @review at 7725078d and again at dc2226de after the rebase. Live in the hub and room deploy at 3d7857d.
  The m1mini worker is culled.
- 2026-09-30, the launch job, three steps, all landed at ffa8b319:
  1. sg3 and m1mini updated to a claude/main build (b8edf0e2), both attached with `git:true`.
  2. f-019 stage 1 live on both rooms: the hub syncs claude/main into each clone, and a room's branches are collected
     into the main checkout as `<room>/claude/*`.
  3. How a director places a worker on sg3 or m1mini: `docs/backlog/fabric/remote-workers.md`.
- 2026-09-30, a refused provision smoke launch prints the room's reason: e4eb68dd.
- 2026-09-30, f-026, a room with no certificate cannot take or relay as a proven name: passed by @review
  (e738577e), landed with lows 3 and 4 fixed. The landing sha is in `notes/director-reports.md`.
