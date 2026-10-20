## Test plan

## @LETTER@. The api tests start from a migrated template

### @LETTER@1. The slow test is fast

1. Run `go test -tags integration -count=1 -run TestARunningRowArrivesQueuedAndIsStarted -v ./internal/api`.

**Expected:** it passes in well under a second, and `node scripts/flaky-suite.js check` no longer lists it.

### @LETTER@2. The package still passes

1. Run `go test -tags integration -count=1 ./internal/api`.

**Expected:** ok, and no `atrium-api-template-*` folder is left in the temp directory.
