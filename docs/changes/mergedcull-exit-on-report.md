## Test plan

## @LETTER@. A work card with an unmerged branch keeps running

### @LETTER@1. Exit on report does not race the merge tests

1. Run `go test -tags integration ./internal/daemon -run TestAWorkCardWhoseBranchIsNotMergedKeepsRunning -count=10`.

**Expected:** every run passes.
