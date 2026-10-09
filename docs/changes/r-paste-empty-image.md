## Test plan

## @LETTER@. A pasted image with no bytes is not uploaded

### @LETTER@1. Empty paste

1. Open a card's terminal on the board.
2. Paste a second screenshot from a snipping tool a minute after a first one.

**Expected:** either the real image goes up, or a toast says the pasted image was empty. No 0-byte file appears
under `.atrium/incoming`, and no path is pasted.
