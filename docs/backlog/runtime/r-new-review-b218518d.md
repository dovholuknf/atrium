# Review: r-move-m1 b218518d

Range `ef406235..b218518d` on `claude/r-move-m1`, one commit. This is stage M1 of docs/rnd/room-handoff-design.md:
the room side of `atrium move`. It touches 24 files:
- New: `internal/daemon/move.go` and `internal/store/move.go`, with their tests and `move_e2e_test.go`.
- Hooks into the say, tell, report, park, attach, reaper and supervisor paths.
- `LaunchRequest.MovedFrom` and `PinOrder`.
- Migration `0075_atrium_move`.

Commits on m1mini are unsigned.

Verdict: **hold** for the room, on M1 to M4. A probe test in scratch shows each of M1 to M3, and M4 is a build failure
on landing. What is built is careful:
- the store's freeze, link and close are each one transaction or statement;
- the files a move writes go through an allowlist and safepath;
- 15 of 16 mutants die.

The holds are the paths that start, type into, or deliver to a card without asking whether a move holds it.

## How it was checked

- I read the diff in full, and the paths it hooks into: `handleResume`, `reopenSaved`, `restartSession`, `tryWake`,
  `typeLabelledThroughGate`, `windDown` and its callers, the control MCP launch bodies, `edge.For` and `safepath`.
- Probe tests in a scratch worktree at the tip. The outputs are quoted under each finding.
- Mutants, run against the move, freeze, chain, say, notice and handle tests. The known reds are filtered out.

  | Mutant | Result |
  |---|---|
  | `askedToLeave` ignored in `fileExitChecked` | killed by `TestAnUndoAfterTheLaunchLeavesTheOldCardAsItWas` |
  | `unpark`'s frozen guard removed | killed by `TestOnlyTheMovesUndoWakesAFrozenCard` |
  | Typing allowed into a frozen card (`attach`) | killed by `TestTypingIntoAFrozenCardIsRefusedAndAcked` |
  | `holdingMessages` ignores the freeze | killed by three tests, the e2e among them |
  | `sayGate`'s frozen answer removed | killed by `TestAParkedFrozenCardTakesTheSayInsteadOfRefusingIt` |
  | Forwarded-say dedup off | killed by `TestAFreezeQueueArrivesOnTheSuccessorOnceAndIsDroppedOnItsAck` |
  | Forward never acks | killed by the e2e and the queue test |
  | `CloseMoved` ignores what is queued | killed by `TestACardWithSomethingQueuedIsNotClosed` |
  | `SetMovedTo` lease check removed | killed by `TestTheLinkIsRefusedWithoutTheFreezeAndAfterTheLease` |
  | 8-hop cap raised to 1000 | killed by `TestAChainIsFollowedForEightHopsAndNoMore` |
  | `followMoved`'s seen-set check removed | **survived** (N1: the hop cap still answers the loop, at hop 8) |
  | A tell to a moved card does not follow it | killed by `TestATellByHandleToAMovedCardFollowsItToo` |
  | A frozen launcher's notice held, not queued | killed by `TestANoticeForAFrozenLauncherIsQueuedNotHeld` |
  | The transcript name allowlist off | killed by `TestUnpackRefusesEveryNameOutsideTheAllowlistAndWritesNothing` |
  | `moved_from` idempotence off | killed by `TestAWorkerAndItsChildMoveFromOneRoomToAnother` |
  | A moved card's handle not reserved | killed by `TestAMovedCardKeepsItsHandle` |

- Tests and checks, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset.
  - store passes.
  - api fails only on `TestTheWalkerLaunchSetAndClear`.
  - daemon fails only on reds that were there before: the hostterm socket set, `TestKeepaliveForkCarries...` and
    `TestNoTestHereCanReachALiveRoom`.
  - vet is clean. gofmt flags only `internal/daemon/fyi_test.go` and `cmd/ptyhost-spike/pipe_windows.go`, and this
    branch changes neither.
- The merge onto `claude/landing` 8bfdcdb1 does not build. See M4.
- The test-plan letter **IM** is unique.

## Their seven points

**(5) Who can reach the move routes.**
- The routes are on the board handler only: the loopback human listener, and the same handler served over the hub
  link. The agent listener's mux is unchanged.
- The loopback board has no credential, only the host and origin checks in `edge.For`. A card with a shell on a room
  that has its own loopback board can therefore reach these routes. It can already reach `/v1/launch`,
  `/v1/tasks/{id}/message` and the stop and delete routes the same way.
- What the move routes add for such a caller:
  - freeze any card, for any lease (L2);
  - read any card's transcript and project memory through `pack`;
  - write allowlisted files into any existing directory through `/v1/move/files`.

  The card's own account can already do all of that on disk, so it is no new class of access.
- A hub-attached room started with no board (`-`) has no loopback board. There, only the hub link reaches these
  routes, and row 12 (who asks) is the hub's job in M2.
- `files` is sound. Every name goes through the allowlist and `safepath.Contained`, symlinks included, before any
  file is written. The total is capped at 256 MiB, and the decode at twice that.
- `pack` follows symlinks (L3).

**(3) `askedToLeave`.**
- `runner.leaving` is set only in `windDown`, which runs on an atrium-typed stop: `stopOne`, stop-all, an idle park,
  and a worktree that is gone. A card has no way to set it for itself or for another card.
- The change also stops a fresh restart after any atrium-typed stop inside the startup window. That is right for all
  of those callers.

**(1) Launch idempotence keyed on `moved_from`.**
- Only the raw board `/v1/launch` body reads `moved_from`. The hub and stdio `atrium_launch` build their bodies
  field by field and leave it out, so a card cannot set it through the control MCP.
- A crafted `moved_from` returns an existing successor without changing it, or marks a new card as a successor,
  which `abort` can then end. Nothing is taken over.
- The key is written late, though (L1).

**(4) A forwarded say reaches the successor before the cut-over note.** Confirmed, see M3.

**(2) `MoveRelay.MovedIn` is optional.**
- A relay without it fails closed, with "not a room linked to a hub that can carry a move".
- M2 has to add it to the link relay. The onward forward in `takeMovedIn` does not need anything more from M2.

**(6) The restart-wake divert is not done.** It is worse than not diverted: a wake is typed into a frozen card. See
M2.

**(7) Held notices for a frozen launcher travel as peer messages.**
- They do: `QueueFromPeer`, sent as the worker.
- After an undo they are replayed as says, typed. Before the freeze they would have been held on the card. That is a
  change of kind for a launcher that holds its notices (N2), but nothing is lost.

## Mediums

### M1: Resume starts a runner for a moved card, and for a frozen card that is not parked

None of the paths that start a card checks `Moved()` or `Frozen()`. `unpark` does, but `handleResume` calls `Launch`
directly for a card that is not parked and has no runner. A moved card is exactly that, because `moveClose` unparks
it and marks it done. So is a frozen card whose runner died before the park step. `reopenSaved`, `restartSession` and
`Launch` with the card's id go through the same open door.

Probes, on the two-room scene in `move_e2e_test.go`:
- After a full cut-over, `POST /v1/tasks/<old>/resume` on room a answered
  `400 {"error":"that handle belongs to a card that moved, and stays reserved"}`. Even so, room a had a live runner
  for the old card 3 s later, resuming the conversation the successor runs on b.
- Freeze, then the runner leaves (`parked=false frozen=true status=needs-input`), then Resume answered
  `200 {"ok":true,"started":true}` and started a runner.

Two runners on one conversation are what the move exists to prevent. On one machine with a shared home folder, they
share one transcript file.

Fix:
- Refuse in `Launch` and `launchLocked` when the request's card is `Moved()`, always, or `Frozen()`. The only
  exception is the undo's own `unpark(ViaMove)`.
- Have `handleResume` answer 409 with where the card went.
- Add one test for each of the two probes.

### M2: a restart wake is typed into a frozen card

`tryWake` checks the runner, the dialog and the turn. It never asks `holdingMessages`, and neither does
`typeLabelledThroughGate`.

Probe: queue a wake, `Freeze`, then `wakeTick`. The terminal got `[atrium] restart wake: ... we up\r`.

A freeze between the check and the park therefore takes a turn the hub does not know about. The transcript and the
capture can change after they were checked. That window is most likely during the rolling restart of section 7,
which is when wakes exist.

Fix:
- `tryWake` returns while `holdingMessages(card)`. The wake stays queued, and the divert to the successor comes later
  (point 6).
- Check the other automatic typers the same way. Idle-tick already checks.
- Add a test.

### M3: forwarded says are typed into the successor before it is told it is live

`takeMovedIn` delivers each item through `deliverPeerWhen` as soon as it arrives. That is cut-over step 3 in the
design, before step 6, the note. The successor's first prompt tells it not to act on anything until that note.

Probe: link, then `forward`. "sent during the freeze" was typed into the successor once, before `adopt`, and with no
note.

The say is acked and dropped on A, so a successor that does as it was told and ignores it loses it. That is the one
thing section 3 says a move may not do.

Fix:
- On the room side, a card with `moved_from` that has not yet had its live step queues forwarded items without
  injecting them.
- An explicit step (`live`, or `adopt` with a flag) releases them in order, after the note.
- Add a test that nothing is typed before that step and everything is typed once after it.

### M4: the branch does not build on landing

The merge onto 8bfdcdb1 conflicts in two places:
- `internal/daemon/launch.go`, where `OutsideCode` from r-hub-remote sits beside `MovedFrom`/`PinOrder`. Keep both.
- `docs/test-plan.md`. Keep both, as IM.

Resolved, it still does not build: `move.go:180: not enough arguments in call to d.sayAcross`. f-cross-room-wake added
a `wake bool`.

With `false` passed, the merged tree builds and vets, and the move, freeze, chain, unpack, pack, hub-git, outside and
wake tests pass, and so do the store tests.

Rebase onto landing. Decide whether a say to a moved card carries the sender's `wake`. Passing it on is my guess at
the intent, and a test should pin it.

## Lows

- **L1: `moved_from` is written after the runner starts.**
  - `spawnPTYResume` runs before `SetMovedFrom`, about 70 lines later in `launchLocked`.
  - If the daemon dies in between, the pty host keeps the successor's process alive, and the card has no
    `moved_from`. The hub's retry then finds nothing and launches a second successor on the same conversation.
  - The design keys the launch on a `task_id` derived from the move id, which `repeatLaunch` already honours.
  - Accept that too, or write `moved_from` when the card row is created.
- **L2: the freeze lease has no cap.** `lease_secs: 315360000` was accepted, `until=2036-09-30`.
  - Only an undo with the move id ends a lease like that. The sweep never reaches it.
  - Cap it at, say, 2 hours. The hub renews it anyway.
- **L3: `pack` follows symlinks.**
  - `memory/*.md` and the cwd's `BRIEF.md`/`HANDOFF.*.md` are read with `os.ReadFile` after a `DirEntry.IsDir()`
    check, which is false for a symlink.
  - A card can link `memory/x.md` to any file its account can read, and the move carries that file to another room's
    account.
  - Carry regular files only (`e.Type().IsRegular()`, or `Lstat`).
- **L4: the migration is named `0075_atrium_move` and sits after `0080`.**
  - It works, because names are compared whole and `0075_work_merged_cull` is a different name.
  - Still, it reads as a duplicate and breaks the order. Rename it to `0081_atrium_move` while nothing has run it.
- **L5: a say can be stranded on a closed card (plausible, not probed).**
  - `handleMessage` reads the card while it is frozen, so `sayToMoved` passes it by.
  - `CloseMoved` commits.
  - The say is then queued on a card that is closed and moved, and nothing forwards it again.
  - Re-check `Moved()` after the queue insert and forward it, or make the insert conditional on the card not being
    closed.
- **L6: a private path in a public test.** `inferrepo_test.go` adds a home-directory path under `/Users/`. Use a
  neutral path such as `/home/x/git/github/org/atrium-worktrees/w`.

## Notes

- **N1:** removing the seen-set check survives, because the 8-hop cap still refuses the loop, just at hop 8. A test
  that a two-card loop is named at its first repeat would pin the earlier answer.
- **N2:** a frozen launcher's held notices come back as typed says after an undo. If the launcher holds its notices,
  replay them as held notices.
- **N3:** for M2, have the hub's move verb be the only caller of these routes, so the generic board pass-through
  cannot skip row 12.

Atrium-Verdict: hold ef406235..b218518d
Quality: a careful room side: one transaction for each step that must not race, an allowlist and safepath on every
file written, and a move that has a test for each of its rules. The holds are in the paths around the move: start,
wake and deliver, which still treat a frozen or moved card as ordinary.
