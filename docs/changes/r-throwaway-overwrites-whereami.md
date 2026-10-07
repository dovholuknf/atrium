## Test plan

## @LETTER@. A throwaway does not take the machine's address

### @LETTER@1. Second room leaves the file alone

1. With the machine's room running, start a second `atrium room` without `--isolated`.
2. Read `daemon.json` in the atrium state directory.

**Expected:** the file still names the first room's pid, the second room's log says it did not record its address, and
a permission hook in a live session still reaches the first room.

### @LETTER@2. Stale file is replaced

1. Stop the room with a hard kill so `daemon.json` stays behind.
2. Start a room.

**Expected:** the room replaces the file with its own address.
