## Test plan

## @LETTER@. The unifiedCard unit waits for the data it measures

### @LETTER@1. The capped done column

1. Run `HEADLESS_UNITS=unifiedCard,bootClean node scripts/test-board-headless.js`.

**Expected:** both pass. The done column is measured only after its header reads 111 and its more row is drawn.
