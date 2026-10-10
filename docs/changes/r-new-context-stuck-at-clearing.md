## Test plan

## @LETTER@. A context cycle does not stall at the clear

### @LETTER@1. Turn ends with subagents out

1. Start New context on a card. Let it write its handoff and run `atrium ready` while a subagent is still running.
2. End the turn.

**Expected:** the strip moves past "2/3: clearing the context", `/clear` is typed, and the wake follows.

### @LETTER@2. A clear that cannot be typed

1. Start New context and keep the card in a turn after `atrium ready` (a long tool call).
2. Wait 5 minutes.

**Expected:** the room log says every 30 seconds what `/clear` waits on, then the chip fails with that reason.
