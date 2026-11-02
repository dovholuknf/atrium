## Test plan

## @LETTER@. Tests that run go build keep the module cache

### @LETTER@1. The hook integration tests build in seconds

1. Run `go test -tags integration ./internal/cli -run 'TestTodaysClaudeHooksPassTheKit|TestEveryInstalledHookFailsOpenFastAndSaysWhy'`.

**Expected:** both pass and the build downloads no modules.
