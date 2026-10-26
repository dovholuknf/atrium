## Test plan

## @LETTER@. A card's parent can be changed

### @LETTER@1. Move a card under another

1. Right click a card nested under the wrong card and choose "change parent…".
2. Pick the card that drives it and press "change it".

**Expected:** the card nests under the new parent and its history shows a parent-changed note.

### @LETTER@2. Refuse a cycle, and clear

1. Choose "change parent…" on a card and pick a card it launched.
2. Choose "change parent…" again and pick "(no parent)".

**Expected:** the first is refused, the second leaves the card with no parent.
