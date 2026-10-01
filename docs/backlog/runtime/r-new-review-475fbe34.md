# Review of r-clear-vs-restart 7c735647..475fbe34 (@runtime: a restart during a new context)

Reviewed by @review, 2026-10-01, from `git diff 7c735647..475fbe34` (aeae397f, 053319d8, 45e2029f, 475fbe34) and a
scratch worktree at 475fbe34. Room side and hub side (internal/link, scripts/live).

Tests at 475fbe34: `go vet ./internal/daemon/ ./internal/link/` passes. `go test -count=1 ./internal/link/` ok
(190 s). `go test -count=1 -run 'NewContext|Context|Attach|Idle|Journal|Restart' ./internal/daemon/` ok (48 s).

## What holds

- **The journal is ordered.** `save()` takes the snapshot under `saveMu` and then `mu`, so two snapshots reach
  `persist` in the order they were taken. The store write runs outside `n.mu`, so no lock is held across sqlite.
- **A graceful stop keeps the journal.** `Shutdown` calls `nctx.stopAll()` before `stopSupervised`. A run that sees
  the stop returns `errNewContextGone`, and `fail` skips that error, so the row is not dropped as failed on the way
  out. A hard kill leaves the row as well.
- **`endAbandonedNewContexts` runs before any runner is reopened**, so nothing types into a card whose clear may or
  may not have happened, and the journal is emptied once each row has its chip and event.
- **The 409 names its step**, and the link refusal names the card, the room and the step. A room that does not
  answer is not counted, and `Wait-NewContextsDone` returns true when the hub does not answer. Both follow the
  hook posture. The scripts exit 0 on "still under way, nothing changed", which matches the existing "restart held
  by the board" exit.
- **`ncBusy`** uses the screen only as a tiebreak when the activity or the status says busy, and it reuses
  `watchLooksIdle`'s signature (silent for `LooksIdleAfter`, settled prompt, no dialog, no subagents). Claude's
  spinner writes while a turn runs, so a turn in progress does not read as silent.

## Findings

### 1. MEDIUM: after a restart at step 2 or 3, the reopened card's own SessionStart removes the seeded chip

`seedFailed` keeps `row.Conv`, the conversation the run BEGAN in. `newContexts.sessionStarted` deletes a failed
chip when a SessionStart names a different conversation. The resume id is recorded on every session event
(shelve.go, `SetResumeID`), so once `/clear` landed and its SessionStart arrived, the card's resume id is the NEW
conversation. The restart reopens the card on that id, its SessionStart names it, and `wakeSawSession` deletes the
chip seconds after startup. The wake (read the handoff) was never typed, and the card shows nothing. Only the
history event says what happened.

This is the case the journal exists for: the capture and the clear are done and the wake is lost. At step 1, or
at step 2 before the SessionStart, the resumed conversation is the old one and the chip stays, which is right.

PROVEN with a scratch test (not committed): journal a row at `wake` with Conv `c1`, call
`endAbandonedNewContexts`, then `wakeSawSession(id, "c2")`. The chip is gone: `the seeded chip was removed by
the resumed card's SessionStart`.

Fix options, in the order I would take them:
- For a row at `wake`, or at `clear` with a SessionStart already seen, the clear is proven: queue the wake text as
  an after-restart wake (restartwake.go already delivers text to a card once its runner's hooks post) instead of
  seeding a chip, or as well as one.
- Or mark a seeded chip as abandoned and let `sessionStarted` skip it, so it goes only by a rerun or a dismissal.

Test: journal a run at `wake` with Conv "c1", call `endAbandonedNewContexts`, then `wakeSawSession(id, "c2")`, and
assert the card still shows the chip or has the wake queued.

### 2. LOW: refused keystrokes are invisible on the board, and the idle parking's capture refuses them too

The board does not handle `in-refused` (no match in internal/api/web), so typing during a new context is dropped
without a word. `holding` is true for the idle parking's capture-only run as well (`capOnly` changes only the
journal). An operator who comes back to a card while it is being parked types into it and loses the input. Either
leave capture-only runs out of the refusal, or file a @ui follow-up to show `in-refused`. Both are better.

### 3. LOW: a capture-only run is briefly journalled

`claim` saves the row before `captureOnly` marks it, so a restart between the two reports an idle parking as an
abandoned new context. The window is two store writes wide. Passing `capOnly` into `claim` (or a
`beginCaptureOnly`) closes it and saves one write.

### 4. NIT

- The startup reason says "after %s" from `row.Since`, which counts the downtime too. "the step began %s ago" is
  what it measures.
- `deploy-hub-only.ps1` waits for new contexts, but a hub restart does not touch a room's terminals. Harmless, since
  it only delays, but the comment says "stopping the room cuts a clear off", which is true only of deploy-batch.

## Verdict

HOLD 7c735647..475fbe34 for finding 1. Re-read 7c735647..<tip> and then room-ok and hub-ok.

Quality: after the Sonnet switch, no drop seen in the parts it set out to do. The ordering (stop before the
supervisor, journal before reopen) is deliberate and right. The miss is the second-order one again: the chip's
clearing rule was written for a live run and was not re-checked against a resumed card.
