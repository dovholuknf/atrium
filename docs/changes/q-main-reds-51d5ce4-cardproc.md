## Test plan

## @LETTER@. InDir matches a working directory spelled another way

### @LETTER@1. A process started through a junction

1. On Windows run `go test -tags integration ./internal/base/cardproc -run TestStopInDir -count=3`.

**Expected:** both tests pass. A process started in a junction to the dir is found and ended by `StopInDir`.
