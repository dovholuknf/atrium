# headless-sharded

`node scripts/test-board-sharded.js` runs again, over all 265 units.

## What changed

- `mockVars()` and `mockSet()` gained `cardColorsMock`, `blockerEsc`, `idleParkNow` and `idleParkWrites`. The last two
  were not in the earlier note and turned up once the listing check below was made to fail.
- Eleven sections had no unit and now have one: termLag, themePickerShots, u002, mReload, mReconnect, mActivityRead,
  mReadRetry, switchBackCost, keepMemory, repliesShots and pullsAbsent. pullsAbsent was only missed by the check, which
  could not read `unit("pullsAbsent",() =>` with no space.
- repliesShots, switchBackCost and keepMemory are tools that need `REPLIES_SHOTS` or `REPLAY`. Without it they return
  and pass, as themePickerShots already did, so the default run does not fail on them.
- The check already existed: the harness's `HEADLESS_LIST=1` names the sections no unit owns and the sharded runner
  stops with that text. A missing mock variable only warned, so it now fails the listing as well, with its message
  saying what to do.

## The run

On m1mini, 8 cores, 6 shards, `--local`, no weights refreshed.

| run | wall time | result |
|---|---|---|
| serial, six chunks of the 247 units (headless-green) | not timed | all passing |
| sharded, first full run | 6m06s (units add up to 32m50s) | 258 of 265 passed, 3 only on retry, 4 failed |
| sharded, after the unit fixes above | 6m25s (units add up to 35m22s) | 260 of 265 passed, 1 flaky, 4 only on retry |

The serial figure is what the units add up to in the sharded run, so about 35 minutes. A plain serial run was not made.

The second run exits 1 because four units failed in the shards, passed alone, and are not on the flaky list. That is
the runner doing what it was written to do. Each passed when run again alone, and which ones fail changes from run to
run (the first run had mPull, clearKeepsPage and settingsOnce instead):

- resumeSpinner, pasteSpinner, pasteBusy, growlPopout (second run)
- mPull, clearKeepsPage, settingsOnce (first run)

These are timing waits that fail with six browsers busy, and are not listed as flaky here.

## Flakes and failures

- boardDocs is on the flaky list. It failed its first try in both runs, and passed on retry in the second. Alone it
  failed 4 of 5 tries (the waitForFunction on the back button, line 24260 now). That is more than a flake: back from a
  document returns to the list and the panel stays open. Probably the board and the test disagree, not fixed here.
- clearMark and mViewer, the other two known flakes, passed in both runs.
- The first run's repliesShots, switchBackCost and keepMemory failures were the missing inputs, fixed above.
