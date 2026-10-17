Docs: none, a test fix.

## What and why

Run 39054604921, windows board-1: `pollsGone` failed in the shard with `a rooms event with a payload did not paint
the second room` and passed alone. Part (b) opens a page, waits for the first room to be listed (a fetch), then
writes a `rooms` event to every hub stream the mock holds open. The page opens its own hub stream at boot, and on
the loaded runner that stream was not open yet when the event was written, so the event reached nobody and the
second room never painted.

The unit now notes how many hub streams the mock holds before the page loads and waits for one more before writing.

## Test plan

## @LETTER@. The rooms event reaches the page

### @LETTER@1. The unit

1. `HEADLESS_UNITS=pollsGone node scripts/test-board-headless.js`, under CPU load.

**Expected:** passes. The second room paints with no fetch.
