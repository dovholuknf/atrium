## Test plan

## @LETTER@. An idle room stays quiet

### @LETTER@1. No process storm

1. Start a room on Windows with ssh sharing on and nothing to change.
2. Watch process creation for two minutes.

**Expected:** about two powershell launches a minute from the room, not six.

### @LETTER@2. Probe rate

1. Open the board's room stats for an idle room.

**Expected:** the latency series is still filled, from ten timer wakes a second.
