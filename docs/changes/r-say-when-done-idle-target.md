## Test plan

## @LETTER@. A done message to an idle session is not held

### @LETTER@1. Idle target

1. Let a session finish its turn and sit idle at its prompt.
2. From another session, say to it with `when: done`.

**Expected:** the message is typed in at once, not after the next turn.

### @LETTER@2. Busy target

1. Say to a session in the middle of a turn with `when: done`.

**Expected:** the message waits and is delivered when the turn ends.

### @LETTER@3. Turn ended without a Stop hook

1. Interrupt a session so its Stop hook never lands, and wait for its card to read looks idle.
2. Say to it with `when: done`.

**Expected:** the message is typed in at once, and so is one held before the flag appeared.
