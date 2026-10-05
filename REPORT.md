# u-new-new-card-late-in-terminals (second pass)

Done.

## Finding
Confirmed by measurement. On the code before 36c6a4ba a `task` event with no whole row waited on the throttled re-read:
the alert came 4654ms and 4662ms after the event (two runs), over the 2000ms limit, so the check fails. With the commit
it is 396 to 561ms. The row and the alert draw from one list in one pass, so alert to row is 0ms both ways. The gap was
event to list, not alert to row.

## Changed
- 36c6a4ba (first pass) compiled and passed as written. Kept.
- `internal/daemon/launch.go`, `daemon.go`: a `starting` set holds a new card's id from registration until the launch
  returns, then the card is published again so the flag clears even if the launch failed.
- `internal/api/api.go`: `IsStarting` hook, `starting` on the task view (omitted when false, and false once supervised).
- `internal/api/web/js/terminal-list.js`: chip reads "starting" for `starting && !supervised`, with its own hover text.
  Click is unchanged.
- `scripts/check-new-card-row.js`: prints timings always, checks the row says starting, `SHOT=` saves a PNG,
  `NO_STARTING=1` mocks an older room.
- Changelog added.

## Tests
- `node scripts/check-new-card-row.js` (NODE_PATH at a scratch `npm i playwright`, cached chromium): pass, 396ms / 0ms.
- Same with settings-spine.js reverted to before 36c6a4ba: FAIL, 4662ms.
- `go test ./internal/api/`: ok. `go test ./internal/daemon/`: 3 failures, all also failing on 90ba936e (TestAnOlder
  ClaudeIsStartedWithoutTheFlagAndTheDetailsSayWhy, TestGlobalAutoSurvivesAReopen, TestIdleParkWorkersFirst, the last
  flaky). Not from this change. Tests that make git commits needed `commit.gpgsign=false` here (no signing key).
- No new Go test for the `starting` flag. The daemon launch needs a real runner.

## PNGs
`docs/screens/u-new-new-card-late-in-terminals/before-joined.png` (row with "joined") and `after-starting.png`
(row with "starting"). Headless board, real JS, mocked endpoints.
