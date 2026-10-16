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

## @LETTER@. A typed say is delivered only when it is read

### @LETTER@1. Forced say mid-turn

1. Have a Claude worker running a long tool call. From its launcher, atrium_say it with `force: true`.
2. Look at the say in the ledger (the say log).

**Expected:** the answer says `typed-queued` and the row's state is `typed`. When the tool returns and the worker takes
the line out of its box, within ten seconds the row reads `delivered`.

### @LETTER@2. Never read

1. Do the same, and keep the tool running for more than `typed_unread_after` minutes.

**Expected:** the row stays `typed` and gains a note that it is still unread.

## Not built: the force-send chord (S-3)

S-0 measured Ctrl+X Ctrl+S on one Claude Code version (2.1.301, Windows) and it does not kill a running Bash call. No
other version and no non-Bash tool was measured, and no live Claude Code could be driven here, so the chord is deferred
(design D4 and D10).
