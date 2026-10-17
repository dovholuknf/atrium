Docs: none, Close keeps its name and its callers. Only what it guarantees on return changed.

## What and why

`go test -tags integration -count=3 -run TestWrappedRingIsTruncatedWithRebasedCuts ./internal/ptyhost/` panicked on
the first repeat, every time:

```
panic: Log in goroutine after TestWrappedRingIsTruncatedWithRebasedCuts has completed: host: 01M54X... exited code=1
```

The stack is the test helper's logger (`testHost` hands `t.Logf` to the host as `Options.Logf`) called from
`Host.await`, the goroutine that waits for a runner to exit and logs its code. `Host.Close`, run by `t.Cleanup`,
killed the runner, closed the pty and returned without waiting for await, so the exit was logged after the test had
completed, which the testing package treats as a panic that ends the whole package run. A counted run only made it
reliable: the first test's await landed on the next test's clock.

The fix is in the product. The host counts every goroutine it starts that may log, a pty's drain and await and a
client's reader and writer, and Close waits for them before it closes `Done`. The idle loop is not counted, since it
is what calls Close when the host goes idle. The wait is bounded at 5 s and logs when the bound is hit, because a
pty read on a platform that did not unblock on close would otherwise hold the host open for good. With the runners
killed first, the wait is tens of milliseconds.

## Test plan

## @LETTER@. The pty host does not log after Close

### @LETTER@1. A counted run of the package

1. Run `go test -tags integration -count=3 ./internal/ptyhost/`.

**Expected:** it passes. No "Log in goroutine after ... has completed" panic.

### @LETTER@2. Close is still quick

1. Run `go test -tags integration -count=1 -v -run TestWrappedRingIsTruncatedWithRebasedCuts ./internal/ptyhost/`.

**Expected:** the test takes well under a second, and the host's log shows "exited code=" before the test ends.
