## Test plan

## @LETTER@. A runner and a shell test ends its runner without taskkill

### @LETTER@1. The test ends its runner promptly

1. Run `go test -tags integration ./internal/daemon -run TestARunnerAndAShellOnOneCardHaveTheirOwnRuns -count=5`.

**Expected:** every run passes and logs no LEAK line.
