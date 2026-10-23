## Test plan

## @LETTER@. The watch-paths test waits for the watcher

### @LETTER@1. Repeat the test

1. Run `go test ./internal/cli -run TestWatchPathsPushesOnlyWhenTheListChanged -count=50 -race`.

**Expected:** every run passes, including under CPU contention (`-cpu 1`).
