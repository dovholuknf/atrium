## Test plan

## @LETTER@. Daemon tests close their database quickly

### @LETTER@1. Restart a missing card

1. Run `go test -tags integration -count=10 -run TestRestartRunnerOnAMissingCardSaysSo ./internal/daemon`.

**Expected:** every run passes and the cleanup never reports that Run had not returned 30s after it was cancelled.
