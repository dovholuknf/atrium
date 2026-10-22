## Test plan

## @LETTER@. The growlOnIt pop-out attach wait does not depend on timing

### @LETTER@1. The unit passes when the page is sampled late

1. Run `HEADLESS_UNITS=growlOnIt,bootClean node scripts/test-board-headless.js`.
2. Add a `pop.waitForTimeout(1500)` before the wait for the pop-out's own terminal and run `growlOnIt` again.

**Expected:** both runs pass. Before the change the second one failed with "the pop-out never attached its own
terminal", because the first card refresh replaces `soloTask` and the old wait needed `termTask === soloTask`.
