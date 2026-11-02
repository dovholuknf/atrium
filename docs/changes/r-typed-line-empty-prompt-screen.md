## Test plan

## @LETTER@. A visibly empty claude prompt never holds a say

### @LETTER@1. Phantom line over an empty prompt

1. Get a Claude card's typing readout to show characters on the line while the prompt on screen is empty.
2. Leave the terminal untouched for about ten seconds, then have a peer `atrium_say` that card.

**Expected:** the line reads empty once the terminal has been quiet, and the message is typed in without waiting for
the next turn.

### @LETTER@2. A draft on screen stays held

1. Type half a line into a Claude card's prompt and stop.
2. Have a peer `atrium_say` that card.

**Expected:** the message stays held, because the screen shows the draft.
