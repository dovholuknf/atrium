## Test plan

## @LETTER@. A held card is always released

### @LETTER@1. The ack never comes

1. Start a new context on a card whose agent will not run `atrium ready`.
2. Wait 30 minutes.

**Expected:** the chip fails with "input was held for 30m waiting for atrium ready, released", and a message
said to the card is delivered once its line is clear.

### @LETTER@2. Release by hand

1. Start a new context, then dismiss the chip on the board.

**Expected:** the hold lifts at once.
