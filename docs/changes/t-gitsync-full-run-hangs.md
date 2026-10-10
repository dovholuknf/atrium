## Test plan

## @LETTER@. The gitsync integration tests finish inside the go test limit

### @LETTER@1. A full package run

1. On a machine with git, run `go test -tags integration -count=1 ./internal/gitsync` with the default timeout.
2. Run it twice more.

**Expected:** all three runs pass in about 3 minutes each, with no timeout and no skipped test.
