## Test plan

## @LETTER@. The landed rule as functions

### @LETTER@1. Branch status is unchanged

1. Run the gitsync integration tests: `go test -tags "integration git" ./internal/gitsync`.

**Expected:** the branch status tests pass without edits.

### @LETTER@2. The rule on a temp repo

1. Run `go test -tags "integration git" -run TestLanded ./internal/gitsync`.

**Expected:** a fast-forward reads ancestry, a cherry-pick reads patch, a reworded cherry-pick with the same files reads
subject, a same-subject commit on other files reads unlanded, a merge commit on the branch is ignored, and a branch
carried only by its changelog fragment reads unlanded. No hub is started.
