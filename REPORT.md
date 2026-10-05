# r-new-resume-says-continue, R5 and R6

Branch claude/r-new-resume-says-continue from bcdbb267. Neither `resume-continue` nor `-NoHold` existed. The escalation
coupling (design section 4) was already in: `heldAged` in `internal/daemon/escalate.go` returns false while
`awaitingWake` is true. It had no test, so one is added.

## R5, the hold remembers who was working
- `store.RoomHold.Working` (JSON `working`, no migration, the hold lives in the `room_hold` setting) and `Worked(id)`.
- `startDeployHold` fills it with every held card whose status is `running` or `needs-permission`.
- A refused gated call adds its card (`noteHoldWorking`, `Store.MarkHoldWorking`), since the hold is what ended its turn.
- `liftAtStartup` wakes only cards in `Working`, and only when `unexpected_exit` is on. The line ends "check `git status`
  first." Each woken card gets a `notified` event with `by: resume-continue` (the event kinds are a closed set, so it is
  a `by`, like the other hold events). Cards not woken have their held messages released at once.
- Tests in `roomhold_test.go`: `TestOnlyACardWorkingAtTheHoldIsToldToContinue` (running, needs-input, and a refused
  card, once only, second startup types nothing), `TestWithUnexpectedExitOffTheLiftWakesNobody`,
  `TestEscalationKeepsWaitingWhileAWakeIsToBeTyped`. `heldRoom` now sets its worker `running`. The plain-restart
  unexpected-exit tests are unchanged and pass.

## R6, the deploy script takes the hold
- `live-common.ps1`: `Set-DeployHold` (POST `http://127.0.0.1:7781/v1/hold`, a 409 counts as held) and `Wait-HoldQuiet`
  (polls GET, 300 s bound, names busy cards).
- `deploy-batch.ps1`: new `-NoHold`. Sets the hold and waits after the new-context wait and before the snapshot and
  stop. `-NoHold` logs a WARNING. A hold that cannot be set logs a WARNING and goes on. `-WhatIf` only says it would.
- Not run against a live room, and no test harness covers it. Both files parse clean with the PowerShell parser.

## Deploy routine must change
- Stop sending the "commit and wait" say before `deploy-batch.ps1`. The hold replaces it. A card told to wait by a say is
  idle at the hold and is correctly not woken.
- A room deploy is needed for R5 (Go). R6 is a script and takes effect when installed with `install-live-scripts.ps1`,
  but until the room runs R5 the hold's lift wakes everyone as before (the old build ignores `working`).
- Order: deploy the room build first, then use the new script.

## Test results
`go test ./internal/...` with the env cleared: daemon fails only TestAnOlderClaudeIsStartedWithoutTheFlag...,
gitsync fails the two known ones. `internal/link` `TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess` also failed. I
did not touch it and did not check it on bcdbb267, so treat it as unconfirmed pre-existing.

## Left
- The board does not yet draw "resumed, told to continue" from the event. The event is on the timeline.
- Design question 1 and 2 taken at their defaults.
