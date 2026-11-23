# TestAnUploadForABusySessionAnswersAtOnce missed its 5s bound under load

## PROVEN (on sgg)

- The test is behind the `integration` tag, so `go test ./internal/api -run TestAnUploadForABusySession` runs nothing without `-tags integration`.
- Not reproducible here. Before the change: 20 solo runs and 60 more beside three concurrent whole-package runs, 0 failures. One whole-package run, 0 failures.
- Phases (lookup, read, write, event) never exceeded about 35ms in total in any run, so multipart parse and the 3MB disk write were not slow here.
- After the change: 20 runs pass, `go vet` and gofmt clean. `check-test-waits.go` flags only `scripts/test-board-headless.js` (120 against baseline 119), which this change does not touch.

## INFERRED

- The store has one db connection (`SetMaxOpenConns(1)`, FIFO) and each event is a durable write. The old writer appended an event every 1ms with no end, so the upload's `Get` and its final `AppendEvent` each queued behind a write whose fsync latency balloons when the box is loaded by 181s of parallel tests. That is the likeliest source of the 5.14s, but it was not observed.
- The writer was unrealistic, so the fix is in the test: a bounded burst of 100 events at 20ms. The 5s bound is unchanged and no product code changed.
