## Test plan

## @LETTER@. Pop-out of a card that is already out

### @LETTER@1. Raise by name

1. Pop a card out of the board.
2. Click pop-out on the same card again, also after reloading the board.

**Expected:** the existing window comes to the front, no dialog shows, and there is still one window for the card.

### @LETTER@2. A window with no name

1. Paste a `#term=<id>` url for a card into a new window.
2. Click pop-out on that card on the board.

**Expected:** no dialog shows, the pasted window is asked to close, and a fresh pop-out of the card is in front.
