## Test plan

## @LETTER@. The coldHandoff unit waits for the event stream

### @LETTER@1. The cold handoff toast

1. Run `HEADLESS_UNITS=coldHandoff,bootClean node scripts/test-board-headless.js`.

**Expected:** both pass. The handoff event is written only once the board's connection badge reads live.
