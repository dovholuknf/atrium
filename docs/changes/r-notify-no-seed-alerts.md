## Test plan

## @LETTER@. A notification is a turn ending, never state found

### @LETTER@1. A week-old wait after a hub restart

1. Leave a card in needs-input for a day, with notify and desktop notifications on.
2. Restart the hub with `atrium run` and let the board reload.

**Expected:** no desktop notification, no toast and no phone notice for that card.

### @LETTER@2. A turn that ends now

1. With the board open and unfocused, let a running card end its turn.

**Expected:** exactly one notification for it.

### @LETTER@3. The bell names a growler by its title

1. With the growler on, let a card ask a question, then open the bell.

**Expected:** the line reads "<card> asked a question", with no "growler:" in front.

### @LETTER@4. An ended growler stays ended past a week

1. Let a card ask a question, then end the growler's reason while the card stays cached.
2. Eight days later, restart the hub.

**Expected:** no growler is raised again for that question, and nothing rings.
