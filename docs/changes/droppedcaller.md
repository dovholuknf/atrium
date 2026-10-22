## Test plan

## @LETTER@. Saving remote access survives a concurrent reader

### @LETTER@1. Save while the room reads

1. On Windows, run `go test -tags integration ./internal/link -run TestADroppedCallerLosesItsStreamsAndOthersKeepThem -count=20`.

**Expected:** every run passes, with no `Access is denied` from renaming `remote_access.json`.
