## Test plan

## @LETTER@. A room keeps the newest paths the hub pushed

### @LETTER@1. The pushed list wins over the welcome's

1. Run `go test -tags integration ./internal/link/conn -run TestAHubPushesChangedPathsToAnAttachedRoom -count=20`.

**Expected:** every run passes and the last list heard has the two new addresses.
