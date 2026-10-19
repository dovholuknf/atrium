## Test plan

## @LETTER@. Headless CPU throttle

### @LETTER@1. The throttle is announced and slows the run

1. Run `HEADLESS_UNITS=walkContext node scripts/test-board-headless.js` and note the TIMING line.
2. Run it again with `HEADLESS_CPU_THROTTLE=4`.

**Expected:** the second run prints `HEADLESS_CPU_THROTTLE=4: every page and popup runs 4x slower`, passes, and its
TIMING is clearly larger. Without the variable no such line is printed.

### @LETTER@2. A missing playwright cannot skip a gated run

1. Hide playwright (rename `node_modules/playwright` and `node_modules/@playwright`).
2. Run the harness plain, then with `HEADLESS_CPU_THROTTLE=4`, then with `CI=1`.

**Expected:** the plain run prints the skip line and exits 0. The other two print `FAIL: playwright is not installed`
and exit 1.
