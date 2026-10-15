## Test plan

## @LETTER@. The Go tests that were red in CI

### @LETTER@1. The four tests

1. Run `go test -tags integration -count=5` on `internal/api` for `TestOpenLiveAnswersTheLiveCardOfAKey` and
   `TestNewWorkWithAnExistingNameAnswersTheWorktreeAndItsLiveCard`, on `internal/daemon` for
   `TestAWedgedWaitForTheAckIsNeverATrap`, and on `internal/cli` for `TestRunMakesThisMachinesRoomOverItsOwnLink`.
2. All pass, on Linux, macOS and Windows.
