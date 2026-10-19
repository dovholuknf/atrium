Docs: none, a fix that makes the warning match what it says

## Test plan

## @LETTER@. A board-only hub deploy does not flag the rooms

### @LETTER@1. Board-only difference

1. Build the hub and a room from the same commit with `make build`.
2. Change a file under `internal/api/web`, build the hub again, and restart it.
3. Open the rooms pill.

**Expected:** no room shows "runs a different atrium build than the hub".

### @LETTER@2. Code difference

1. Change a Go file, build the hub again, and restart it. Leave the room on the old binary.

**Expected:** the room row says "runs different atrium code than the hub" with both short commits and which is newer.
