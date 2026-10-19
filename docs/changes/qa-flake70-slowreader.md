## Test plan

## @LETTER@. Slow reader test is stable on Linux

### @LETTER@1. Run the test

1. Run `go test -tags integration ./internal/ptyhost -run SlowReader -count=1 -v`.

**Expected:** it passes. The test is the only change, so no product behaviour moves.
