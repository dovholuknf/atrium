## Test plan

## @LETTER@. idleRate waits for the board to drain

### @LETTER@1. After the room answers

1. Run `HEADLESS_UNITS=idleRate,bootClean node scripts/test-board-headless.js`.

**Expected:** both pass. After the stalled room is released, each of the three pages reaches no fetches in flight and
none queued within 5 page seconds, and the unit fails naming the page that did not.
