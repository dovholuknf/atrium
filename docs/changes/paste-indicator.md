## Test plan

## @LETTER@. A big paste shows pasting until it lands

### @LETTER@1. Paste a long log into a terminal on the hub board

1. Open a card's terminal on the hub board.
2. Paste a log of a few thousand lines, about 500 KB.

**Expected:** the "pasting" box shows at once at the top right and stays up while the runner prints the paste back.
It goes down a moment after the output stops.

### @LETTER@2. A runner that only redraws a spinner

1. Paste a short line into a terminal whose runner is busy redrawing.

**Expected:** the box goes down after its short hold, not after the runner goes quiet.
