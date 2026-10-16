## Test plan

## @LETTER@. The unit budget is green on daemon and cli

### @LETTER@1. Unit run

1. Run `bash scripts/test-touched.sh --base <a commit before a daemon and cli change>`.
2. It passes with no test over the 20ms budget in `internal/daemon` or `internal/cli`.

### @LETTER@2. Moved tests still run

1. Run `go test -tags integration ./internal/daemon ./internal/cli -run 'TestScopeHubRemote|TestRemoteAccess|TestWalkPack'`.
2. They pass.
