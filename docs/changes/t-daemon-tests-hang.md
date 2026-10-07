## Test plan

## @LETTER@. The daemon's tests finish inside CI's timeout

### @LETTER@1. The whole package on Windows

1. `go test -count=1 -timeout 20m ./internal/daemon/` on a Windows machine.

**Expected:** it ends with `ok` or a list of failures, never `panic: test timed out`. It takes about 7 minutes.

### @LETTER@2. The whole package on Linux

1. `docker run --rm -v <worktree>:/src -w /src -e GOTOOLCHAIN=auto golang:1.26 go test -count=1 -timeout 20m ./internal/daemon/`

**Expected:** `ok` in about 4 minutes.

### @LETTER@3. CI

1. Push a branch and open its ci run on GitHub.

**Expected:** the go test step finishes on windows-latest. If internal/daemon fails, it is a test failure and not
`test timed out after 10m0s`.

### @LETTER@4. No leaked attach after a reattach

1. `go test -count=1 -run TestACrashBetweenFilingAndCollectingFilesNothingTwice ./internal/daemon/`

**Expected:** `ok`. The test ends by checking that no `ptyhost.(*evq).pump` goroutine is left once the daemon closes,
which is what a run that had ended while no daemon was connected used to leave behind for good.
