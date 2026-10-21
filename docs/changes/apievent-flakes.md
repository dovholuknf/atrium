## Test plan

## @LETTER@. Two flaky api tests

### @LETTER@1. Event equals list row

1. Run `go test -tags integration -count=5 -run TestTaskEventEqualsListRow ./internal/api`.

**Expected:** passes every time, including when the two renders straddle a second boundary.

### @LETTER@2. Close counts an ignored tree

1. Run `go test -tags integration -count=5 -run TestCloseRemovesAnIgnoredDeepTreeAndCountsItsBytes ./internal/api`.

**Expected:** passes every time, and the worktree row keeps the size the close measured even when the open's background
measure finishes late.
