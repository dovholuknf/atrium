## Test plan

## @LETTER@. A board link in a terminal stays in the board

### @LETTER@1. In the board

1. Print `<board origin>/#term=<card id>` in a terminal and click it.

**Expected:** that card's terminal is shown in the same window and no new tab or window opens.

### @LETTER@2. From a popped-out terminal

1. With the board open, click the same link in a popped-out terminal.
2. Close the board and click it again.

**Expected:** the board comes forward on the card. With no board open, one opens on the card.

### @LETTER@3. Foreign links

1. Click `https://example.com/` in a terminal.

**Expected:** it opens in its own named tab as before.
