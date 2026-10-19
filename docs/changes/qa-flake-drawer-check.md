## Test plan

## @LETTER@. The drawer check lives in the flaky suite

### @LETTER@1. The manifest names the script

1. Run `node scripts/flaky-suite.js check` and `node scripts/flaky-suite.js scripts`.
2. The first prints `ok`, the second prints `check-board-drawer` and its path.

### @LETTER@2. The gating board step skips it

1. Run `SKIP_DRAWER_CHECK=1 bash scripts/check-board.sh` with `HEADLESS_UNITS=bootClean`.
2. The filter drawer check does not run. Without the variable it does.

### @LETTER@3. The drawer check passes alone

1. Run `node scripts/check-board-drawer.js`.
2. It prints no FAIL line.
