## Test plan

## @LETTER@. The cleanCopy unit has no flat waits

### @LETTER@1. Ratchet and unit

1. Run `go run scripts/check-test-waits.go`. It exits 0 and `scripts/test-waits-baseline.json` is unchanged.
2. Run `HEADLESS_UNITS=cleanCopy node scripts/test-board-headless.js`. It says the units asked for passed and prints a
   TIMING line.
