## Test plan

## @LETTER@. Two daemon tests hold under load

### @LETTER@1. Run them with one CPU

1. Run `go test -tags integration -cpu 1 -count=3 -run TestA ./internal/daemon`.

**Expected:** `TestALiveRefitRestoresTheCursorOnlyWhenThePtyMoves` and
`TestASayReachesAnIdleRunnerAfterADeployRestartWithoutForce` pass every time.
