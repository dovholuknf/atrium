Docs: none, a test fix.

## What and why

Run 39052523653, windows board-1: `pullsAbsent` failed in the shard with `the board never asked for /v1/prs` and
passed alone. The unit waited for `pulls.loading` to be false and then half a second, and counted the requests. The
first read of `/v1/prs` starts when the event stream opens (`onPullsStreamOpen`), so before it has begun "not
loading" is already true, and on the loaded runner the stream opened later than the half second.

The unit now waits for the read to end, `pulls.loaded || pulls.loadFailed`, which the 404 the unit serves sets.

## Test plan

## @LETTER@. The pulls tab stays hidden without /v1/prs

### @LETTER@1. The unit

1. `HEADLESS_UNITS=pullsAbsent node scripts/test-board-headless.js`, under CPU load.

**Expected:** passes. The board asked once, the tab is hidden, nothing threw.
