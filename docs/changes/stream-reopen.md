## Test plan

## @LETTER@. A reopened event stream still streams

### @LETTER@1. Close and reopen in a loop

1. Run `go test -tags integration ./internal/link -run TestAStreamReopenedRightAfterACloseStillStreams -count=200`.

**Expected:** every run passes, each of the 25 reopens receives a `task` event.
