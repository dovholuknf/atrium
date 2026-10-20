## Test plan

## @LETTER@. No OpenConsole pile after a deploy

### @LETTER@1. A deploy leaves no idle hosts

1. On Windows with Windows Terminal as the default terminal, count `Get-Process OpenConsole`.
2. Run a hub deploy, then count again.

**Expected:** the count is the same as before, apart from hosts belonging to terminals you opened.

### @LETTER@2. The guard

1. Run `go test ./internal/nowindow -run TestEveryExecStartIsHiddenOrListed`.

**Expected:** it passes, and fails when a new `exec.Command` in non-test code is neither passed to `nowindow.Hide` nor
listed with a reason.
