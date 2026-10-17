Docs: none, a test changed. Nothing a reader names changed.

## What and why

`internal/daemon.TestASayToADoneCardIsKeptThenTypedInWhenWakeResumesIt` failed on the windows-latest runner (run
39032382613) with "undelivered 1 after typing, want 0", and one second later the log said the kept text was typed
"but could not mark it delivered: store is closed". The goroutine dump had `deliverKept` inside `markDelivered`,
mid-insert. The test waited for the typed "\r", slept a fixed three kept ticks (60 ms) and read `UndeliveredCount`.
On the runner, 449 goroutines on 4 cpus, the insert took longer than that and the test ended with the store closed
under it.

The product did what it promised: the text was typed once and the row was being marked. Only the test's wait was a
guess. It now polls `UndeliveredCount` to zero on a 5 s deadline in 10 ms steps, the same shape as the poll for the
"\r" just above it. The 5 s is a ceiling the poll leaves the moment the count is zero, not a sleep. Passed `-count=3`
locally. The step comments in the test were cut per the comment rule.

## Test plan

## @LETTER@. The kept text is marked delivered once typed

### @LETTER@1. The daemon test

1. Run `go test -tags integration -count=3 -run TestASayToADoneCardIsKeptThenTypedInWhenWakeResumesIt ./internal/daemon`.

**Expected:** passes every time, with no "could not mark it delivered: store is closed" line in the output.
