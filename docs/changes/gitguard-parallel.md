## Test plan

## @LETTER@. TestGitGuard runs in parallel

### @LETTER@1. The guard cases

1. Run `go test -tags integration -count=1 -run '^TestGitGuard$' -v ./internal/guard`.
2. Run `node scripts/flaky-suite.js check`.

**Expected:** every repo group passes as its own subtest in a few seconds, and the check names no entry that is missing.
