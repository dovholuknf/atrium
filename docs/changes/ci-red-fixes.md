## Test plan

## @LETTER@. CI red fixes

### @LETTER@1. The new-context race

1. `go test -tags integration ./internal/daemon -run TestAWedgedWaitForTheAckIsNeverATrap -count=300 -cpu 1,2`.

**Expected:** every run passes. Before, about one in a hundred typed the limit prompt a second time.

### @LETTER@2. The Windows checks

1. `bash scripts/ci.sh --only ps-tests,gofmt` and `HEADLESS_UNITS=sshBadge,roomsSetup node scripts/test-board-headless.js`.

**Expected:** all pass.
