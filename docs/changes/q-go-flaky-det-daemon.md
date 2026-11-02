## Test plan

## @LETTER@. A start's run row test cleans up without taskkill

### @LETTER@1. The test ends its runner promptly

1. Run `go test -tags integration ./internal/daemon -run TestAStartWritesItsRunRowBeforeTheLaunchIsComplete -count=5`.

**Expected:** every run passes in under a second and logs no LEAK line.
