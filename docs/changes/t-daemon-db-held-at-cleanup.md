## Test plan

## @LETTER@. A daemon test leaves no database open at cleanup

### @LETTER@1. The two tests CI caught, repeated

1. On Windows, `go test -count=5 -run "TestAutoModeDoesNotOverrideANeverRule|TestALaunchOntoALiveCardLongAfterItStartedIsStillRefused" ./internal/daemon/`

**Expected:** `ok`. No `TempDir RemoveAll cleanup: unlinkat ...atrium.db` line.

### @LETTER@2. The whole package

1. On Windows, `go test -count=1 -timeout 20m ./internal/daemon/`

**Expected:** `ok`, and no `is still held 10s after the daemon closed` line.
