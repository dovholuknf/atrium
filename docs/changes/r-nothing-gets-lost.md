## Test plan

## @LETTER@. Nothing a session says is lost

### @LETTER@1. A say to a parked card is kept

1. Park a card. From another session, `atrium_say` it without `wake`.
2. Look at the card on the board and at `/v1/tasks`.

**Expected:** the say answers `queued` with `reachable: kept`, the card stays parked, and the card shows
`messages_undelivered: 1`.

### @LETTER@2. A say to a done card is kept, and wake resumes it

1. Finish a card so it has no session. Say something to it without `wake`.
2. Say something else with `wake`.

**Expected:** the first is kept on the card. The second resumes the conversation, and both are typed in or carried by
the hooks. The undelivered count drops to zero once the session has read them.

### @LETTER@3. Kept messages survive a restart

1. Keep a message on a parked card, then restart the room.

**Expected:** the message is still on the card and is delivered when the card next runs.

### @LETTER@4. A given-up cross-room say comes back

1. Say to a card on a room that is offline and let the held say pass its day.

**Expected:** the sender receives a message carrying the words that were not delivered.
