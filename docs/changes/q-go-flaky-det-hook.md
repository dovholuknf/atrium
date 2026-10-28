## Test plan

## @LETTER@. The unreachable hook test holds a late hook to its own budget

### @LETTER@1. A loaded runner

1. Run `go test -tags integration ./internal/cli -run TestEveryInstalledHookFailsOpenFastAndSaysWhy -count=3` while
   the machine is busy with parallel builds.

**Expected:** it passes. A hook is late only when it took more than hookBudget and 300ms past the startup of the same
binary, three runs in a row. A hook that waited out a dial timeout still fails.