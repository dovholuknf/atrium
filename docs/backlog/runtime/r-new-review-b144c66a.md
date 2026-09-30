# r-new-review-b144c66a. Fix for the ff747683 review: exit-asked is a column

Status: open, one medium and two lows. Filed by @review 2026-09-30. Read-only review of b144c66a (merge of
`claude/r-review-fixes`), the fix for the three findings in `r-new-review-ff747683.md`. Owned by @runtime.

The three findings are closed:

- **The record cannot be routed away.** `task.exit_asked_at` (`internal/store/schema.go`, `0076_task_exit_asked`) is
  appended last, tolerates an existing column, and backfills from the `exit-asked` events ff747683 wrote, only where
  the newest of those and a launch is the ask. `ExitAsked` reads the column.
- **Terminate records it.** `Kill` sets it. A `/exit` typed in a supervised card's own terminal sets it, because the
  `end` hook sees `prompt_input_exit` while `windingDown` is false (`internal/daemon/session.go`).
- **`/resume` refusals** are 409 for a held conversation and 400 otherwise, never 500 (`park.go`).

## 1. Medium. Every exit atrium types itself now counts as somebody deciding

`exitKeysFor` is how atrium asks any runner to leave, and the runner's `SessionEnd` says `prompt_input_exit` whoever
typed it. `windingDown` excludes the shutdown and nothing else. So the new rule in the session hook also sets
`exit_asked_at` for:

- the idle park (`idletick.go:37`, `idleLeave`)
- a runner whose worktree went away (`worktreegone.go:148`)
- the stop half of a restart or a new context (`RestartRunner` goes through `StopRunner`)

The last one is cleared again by the relaunch, which comes after the hook, because the hook posts before the process
exits and the relaunch waits for the process to go. The idle park is not cleared. A fixture whose card was parked for
idleness does not start at the next boot, and its row says "its card was asked to exit", which nobody did. A parked
card is woken by a say or by `/resume`, which clears it. So nothing is lost, but the boot rule and the message are
wrong about why.

Fix: record the ask where atrium decides, not in the hook. Every path that types `exitKeysFor` on purpose already
knows whether it is a decision (`StopRunner` and `Kill` are, the idle park and the worktree sweep are not). Have the
supervisor note "atrium asked this runner to leave" before it types, and have the hook's rule skip a runner so noted.
Then the hook's rule means only a person typing `/exit`.

## 2. Low. Only `launchLocked` clears it

The event rule it replaced counted every `launched` event as a launch after the ask, including `atrium join`
(`session.go:273`) and the reaper reviving a card atrium still owns (`reaper.go:85`). The column is cleared only in
`launchLocked`. A card somebody exited and then rejoined by hand from a terminal is running with `exit_asked_at`
still set. It is not supervised, so reopen never looks, but a fixture on that card stays down at boot. Clear it
beside each of those `launched` events.

## 3. Low. A failed terminate leaves the card marked while it runs

`Kill` sets the column before it signals. When the kill fails and the process is still alive ("could not stop
process"), the card is running and marked. If the room restarts before anything else launches it, the card is not
reopened. Set it after the kill succeeds, or clear it on that error.

## Tests

`go test ./internal/store/ -run 'Exit|Migrat'` passes, including the new `exitasked_test.go`. `./internal/daemon/
-run 'Reopen|Restart|Exit|Fixture|Resume|Ended|Asked|Held|Park|Prompt|Idle' -timeout 20m` passes (59s). No test
covers finding 1. The case: a fixture's card parked by `idleLeave`, then `startFixtures`, then check that it starts
or that its row says it is parked.
