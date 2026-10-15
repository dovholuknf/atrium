## Test plan

## @LETTER@. Held messages are typed once

### @LETTER@1. Several says held for a card, a retry mid-typing

1. Hold three says for a card with a terminal, for example by sending them while its turn is running.
2. End its turn and, while they are being typed, press a key on its terminal.

**Expected:** each say arrives once, in order, never twice.

### @LETTER@2. Close keeps a shared clone and says why

1. Open a pull request link on a room with no checkout, so atrium clones the repository.
2. In that clone add a worktree of your own, then close the card.

**Expected:** the clone is kept, and the close says "1 other worktree hangs off it", on macOS and on Windows too.
