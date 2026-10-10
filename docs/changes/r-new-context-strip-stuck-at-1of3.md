## Test plan

## @LETTER@. The new-context strip follows the cycle to its end

### @LETTER@1. A popped-out terminal

1. Pop a card's terminal out into its own window, then start a new context on the card.
2. Watch the strip under the terminal while the cycle runs.

**Expected:** the strip moves through 1/3, 2/3 and 3/3, and when the wake prompt has been typed the strip and its
"let me type" button are gone.

### @LETTER@2. The compose box when the cycle ends

1. Start a new context, type a key into the terminal so the compose box opens, and leave text in it.
2. Let the cycle finish.

**Expected:** the box closes, and its text is sent to the card at once.
