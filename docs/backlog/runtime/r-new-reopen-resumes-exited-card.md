# r-new-reopen-resumes-exited-card. A room restart brings back a card that was exited, into another card's conversation

Status: fixed 2026-09-30, room side, live at the next room restart. Owned by @runtime. Filed by the orchestrator 2026-09-30.

## What happened

At 11:10 the orchestrator moved from claude-sg4 to a new card on sg4-control (01a0f2da). At 11:20 it exited the old
card on claude-sg4 (atrium-87300, 01a06dc7), which went to `done`. The 11:28 claude-sg4 room deploy started that old
card again anyway (pid 21320, `needs-input`). The resumed session then sent a new-context request that reached the
orchestrator.

Both cards carry the same `resume_id`, `c6fdd51f-fc37-438d-9149-3d2bac73868a`, which is the conversation the
sg4-control card is running. So the old card came back as a second copy of a conversation that another card, in
another room, owns and has open.

## Two bugs, maybe one cause

1. **`reopenSaved` resumes a card whose status is `done` after an exit.** An exit is a human or a launcher saying
   "this is over". A restart must not undo it. Only cards that were running when the daemon stopped come back.
2. **Two cards share one `resume_id`.** Same shape as r-021 part 2 (the 21:11 2026-09-29 restart that resumed a
   @merge conversation on the orchestrator's card). Here the id crossed rooms: the new card's conversation id was
   written onto the old card, probably through the shared `D:/git/github/dovholuknf/atrium` worktree and the session
   hook matching by cwd. A resume id that is already bound to a live card, on any room, must not be bound again or
   resumed.

## Done when

- A card exited before a room restart stays `done` after it. Test: exit, restart, check status and that no process
  started.
- Binding a resume id already held by another card is refused and logged. Resuming one is refused too.
- The fix names which of the two above was the cause here, with the evidence.

## The cause

**Neither guess. It was the fixture, and it caused both.** The room log for 11:28 says
`fixture "atrium" started as 01a06dc7-...`: card 01a06dc7 is the `atrium` fixture's card, so `startFixtures` started
it, not `reopenSaved`. `startFixture` launches onto the fixture's card whatever its status, so a `done` card came back.
The fixture's default resume mode, `latest`, resumes the newest conversation in its directory. The directory is the
shared checkout `D:/git/github/dovholuknf/atrium`, where the orchestrator's new card on sg4-control was running
`c6fdd51f`, so that conversation was resumed onto the old card and its SessionStart bound the id there. No hook
matched by cwd.

## The fix

- `startFixtures` does not start a fixture whose card is `done` at boot. The fixture's row says why, and starting it
  from the fixtures page still works, because that is somebody asking now.
- `reopenWanted` skips a `done` card too, for a runner still at its prompt when the daemon stopped.
- A fixture's `latest` resume, and a reopen's resume, refuse a conversation another live card on this room holds, and
  log which. Binding one was already refused by `ClaimResumeID` (r-021 part 2).
- NOT COVERED: a live holder on ANOTHER room on the same machine. A room cannot see another room's cards. The boot rule
  closes this incident's path. The general case needs the hub, and is named here rather than guessed at.

Tests: `internal/daemon/reopen_exited_test.go`.
