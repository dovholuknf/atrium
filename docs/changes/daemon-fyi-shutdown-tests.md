## Test plan

## @LETTER@. Three daemon fyi tests are off the flaky suite

### @LETTER@1. The tests pass

1. Run `go test -tags integration -count=1 ./internal/daemon -run 'FYI|ANeedsReport|Hold'`.
2. Run `node scripts/flaky-suite.js check`.

**Expected:** every test passes, and the check reports that all entries name a test that exists.
