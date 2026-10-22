## Test plan

## @LETTER@. A phone fit does not re-enter itself

### @LETTER@1. Open a card on a phone

1. Open a terminal card in a phone-sized viewport.
2. Pinch to zoom the font, then rotate the phone.

**Expected:** the page stays responsive with no "Maximum call stack size exceeded" in the console, and a zoom keeps the
font the reader chose instead of snapping back to the width fit.

### @LETTER@2. A web font lands late

1. On a desktop, open a card and let the web font load after the first fit.

**Expected:** the rows are refitted once to the new cell height, as before.
