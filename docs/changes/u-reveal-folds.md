## Test plan

## @LETTER@. Going to a card opens the groups it hides in

### @LETTER@1. The switcher

1. Fold a heading in the terminals list that holds a card, and fold the board column the card is in.
2. Press the switcher key, type the card's name and press Enter.

**Expected:** every heading above the card in the terminals list is open and the row is scrolled into view. On the board the column and the card's group are open too.

### @LETTER@2. A notification or a card link

1. Fold the groups that hold a card that is asking something, then click its notification or toast.

**Expected:** the same as above. Nothing is left folded between the card and the top of the list.

### @LETTER@3. It stays open

1. After going to a card, reload the page.

**Expected:** the groups that were opened are still open, as if they had been opened by hand.
