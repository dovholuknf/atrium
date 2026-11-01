## Test plan

## @LETTER@. The after-a-failed-dial host test is steady

### @LETTER@1. Run it alone

1. Run `go test -tags integration -run TestARunnerLaunchedAfterAFailedDialStillGoesToTheHostOnceItIsUp -count=10
   ./internal/daemon`.

**Expected:** every run passes, and the second runner is in the host.
