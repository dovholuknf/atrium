Docs: none, the fix makes the gate match what agent-messaging.md already says

## Test plan

## @LETTER@. A say reaches an idle runner after a room restart

### @LETTER@1. Idle claude worker

1. Leave a claude worker idle at an empty prompt in needs-input, then restart the room with `atrium stop` and start it.
2. Wait about five seconds, then `atrium_say` to the worker.

**Expected:** the say is typed into its terminal and does not stay queued.

### @LETTER@2. Forced say across rooms

1. From a launcher in another room, `atrium_say` with `force: true` to a worker the launcher spawned.

**Expected:** the target room types it past the gate.

### @LETTER@3. Held say flagged

1. Hold a say behind a half typed line for over five minutes.

**Expected:** the card shows the `!` chip with `held_stuck` and the sender gets one note.
