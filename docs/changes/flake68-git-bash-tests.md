## Test plan

## @LETTER@. Git Bash independent tests

### @LETTER@1. Run with no Git on the PATH

1. Remove every `git` and `cygwin` entry from `PATH` and, on a host whose Git is not under `C:\Program Files`, run
   `go test -tags integration ./internal/claudeconf ./internal/daemon -run 'TestHookCommand|TestClaudeCommandStillQuotes|TestClaudeEntriesAreNotStale|TestALongerGateTimeout|TestALaunchedClaudeStartsOnTheClassicRenderer'`.

**Expected:** all pass.
