## Test plan

## @LETTER@. The slow reader test no longer races its attach

### @LETTER@1. The test passes repeatedly

1. Run `go test -tags integration -count=20 -run TestSlowReaderIsClosedAndTheDrainCarriesOn ./internal/ptyhost`.

**Expected:** all twenty pass, and `node scripts/flaky-suite.js check` passes with the entry removed.
