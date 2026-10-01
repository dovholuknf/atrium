# Review of r-036b 8f6f1e56 (@runtime: wait out a denied claude lock Mkdir on Windows)

Reviewed by @review, 2026-10-01, from `git show 8f6f1e56` on 7c735647 and a scratch worktree at 8f6f1e56. Room side
(internal/runnersetup runs at launch).

Tests at 8f6f1e56, on Windows (sg4): `go vet ./internal/runnersetup/` passes. `go test -count=20 -run
'TestConcurrentClaudeWritesKeepEveryChange|TestLock'` passes 20 of 20 for each of the three tests. `-cpu 1 -count=20`
on the concurrent test passes 20 of 20, slowest run 2.4 s, so no run waited out the 5 s `claudeLockWait`, which is
the shape the first attempt was backed out for.

## What holds

- **The diagnosis is better than the first attempt's.** The backlog entry says the unlock `Remove` never failed
  (instrumented over 60+ runs) and the denied answer is a waiter's `Mkdir` racing a delete pending. That rules out
  the guess in my earlier review.
- **A permission error that persists is still reported as itself** at the deadline, not as "a claude session held",
  and `TestLockReportsAPersistentDeniedMkdir` locks that in.
- **The stale check still runs on a denied answer.** `os.Stat` of a delete-pending directory fails with not found
  (which the entry also observed), so it cannot remove a lock it should not.

## Findings

### 1. LOW: a real permission error now costs the full wait on every OS

The first attempt retried `ErrPermission` on Windows only. This one retries it everywhere, so on Linux or macOS a
home directory the runner cannot write turns an instant failure into a 5 s stall before the same error. The error
is still correct, so this is a cost and not a bug. Gate the retry on `runtime.GOOS == "windows"`, as `lockBusy`
did, or say in the comment why every OS waits.

## Verdict

ROOM DEPLOY OK 7c735647..8f6f1e56. Finding 1 can be a follow-up.

Quality: after the Sonnet switch, no drop seen. The cause was proven by instrumenting it rather than guessed, and
the second attempt is smaller than the first.
