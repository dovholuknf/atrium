## Test plan

## @LETTER@. The held chip follows what is pending

### @LETTER@1. Chip clears when nothing is pending
Hold two peer messages on a card with a half-typed line. Deliver both by the Stop hook. The `!` chip is gone at the
next read, not at the next retry tick.

### @LETTER@2. Partial delivery
Deliver one of two held messages. The chip count drops to 1.
