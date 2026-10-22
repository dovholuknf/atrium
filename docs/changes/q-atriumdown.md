## Test plan

## @LETTER@. atriumDown waits for the worker to be ready

### @LETTER@1. The reload with atrium down gets down.html

1. Run `HEADLESS_UNITS=atriumDown node scripts/test-board-headless.js`.

**Expected:** the unit passes and prints a TIMING line. The reload with atrium down shows "atrium is not running".
