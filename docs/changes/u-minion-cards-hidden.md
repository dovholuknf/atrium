## Test plan

## @LETTER@. Item minion cards hidden, design

### @LETTER@1. Read the design

1. Open `docs/ui/minion-cards-hidden-design.md`.

**Expected:** it answers the six questions (the tag and the filter, how a hidden card still reaches the human, the item
chip, the debug toggle, an item gone or done, the phased build) each with a file and function, and lists open questions
with a recommended answer each. No code changed in this item.

## @LETTER@. Item minion cards hidden, phases 1 and 2

Hidden minions come out of the Terminals tab only. The board, stack and phone are phase 3, the item chip phase 4.

### @LETTER@1. A quiet minion is not listed

1. Launch an item so its minion session starts (tagged `atrium:minion`).
2. Open the Terminals tab.

**Expected:** your own sessions are listed and the minion is not. The header working count still counts it.

### @LETTER@2. A minion that asks un-hides

1. Have a minion raise a question or a permission request.

**Expected:** its row appears in the Terminals tab with a `minion` chip and goes away once answered. The permission and
growl alerts fire as before.

### @LETTER@3. The attached minion stays

1. From the item's page open the minion's terminal chip.

**Expected:** the terminal opens and its row stays in the list while it is attached.

### @LETTER@4. A resting minion is quiet

1. Let a minion sit at rest between turns without a question.

**Expected:** no ready alert rings for it.

## @LETTER@. Item minion cards hidden, phase 4 (the item chip)

### @LETTER@1. A minion chip says what it is doing

1. Open an item whose minion is running, then its detail page.

**Expected:** the minion's chip reads `running`. When the minion asks a question or needs a permission it reads
`needs you` or `needs permission` in orange, and once the minion has exited it reads `ended`.

### @LETTER@2. The chip opens a hidden minion

1. Click the chip of a minion that is not listed in the Terminals tab.

**Expected:** the Terminals tab opens with that minion attached and its row listed while it is open.

### @LETTER@3. A removed card

1. Open an item whose card the board no longer holds.

**Expected:** the chip is struck through, shows `this card has ended and was removed` on hover and cannot be clicked. The
phone's item rows show the same words and colours.
