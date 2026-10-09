## Test plan

## @LETTER@. A say to a busy claude session is carried by a hook

### @LETTER@1. Say mid-turn

1. Have a claude worker running a long command.
2. From another session, atrium_say it "stop and report" with the default `when`.

**Expected:** the answer says `queued`, nothing appears in the worker's terminal or its queued-message box, and the
worker reads the text at its next tool call or when its turn ends.

### @LETTER@2. Say while idle

1. Let the worker finish its turn and sit idle.
2. atrium_say it something.

**Expected:** the text is typed and the answer says `terminal`, as before.
