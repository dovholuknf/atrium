Docs: none, a fix that makes the code match what the docs already say

## Test plan

## @LETTER@. A runner's whole tree dies on a kill

### @LETTER@1. Kill a runner that has started a child

1. On Windows, with `pty_host` on, launch a runner that starts a child process.
2. Kill the runner from the board.

**Expected:** the child ends with it, every time, however soon after launch the child started.

### @LETTER@2. Start outside a job

1. Run `go test -tags integration -run 'TestDetachStartedInAJob|TestStartOutsideJob' ./internal/detach` as a user WMI
   lets create processes.

**Expected:** all three run and pass. Where WMI answers access denied they skip and say so.
