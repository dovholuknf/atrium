## Test plan

## @LETTER@. A submitted line does not leave a phantom on the typing gate

### @LETTER@1. Submit without Enter

1. Paste a long text into a Claude card's prompt and submit it by a path that is not Enter (the board compose bar).
2. Let the turn finish, then have a peer `atrium_say` that card.

**Expected:** the message is typed in once the turn ends, with no "chars on your line" hold.

### @LETTER@2. A draft survives

1. While a turn runs, type half a line into the prompt.
2. Let the turn end, then have a peer `atrium_say` that card.

**Expected:** the message stays held, because the draft is on the line.
