## Test plan

## @LETTER@. The switcher finds dead cards

### @LETTER@1. An ended card

1. End a session so its card has no runner.
2. Open the switcher and type part of that card's name.

**Expected:** the card is listed under every live match, with an "ended" chip. Enter opens the card, not a new
session.

### @LETTER@2. An archived card

1. Archive a card.
2. Open the switcher and type part of its name.

**Expected:** after a moment the card is listed with an "archived" chip, and Enter opens it read-only.

### @LETTER@3. Nothing typed

1. Open the switcher and type nothing.

**Expected:** only live sessions are listed.
