## Test plan

## @LETTER@. Copy clean after a reattach

### @LETTER@1. A selection made after a reattach is the text on screen

1. Open a card's terminal, scroll back a few lines, then stop and start the room so the socket reattaches.
2. Drag-select a paragraph of a reply and click "copy clean".

**Expected:** the button appears beside the selection and pastes the reply's own markdown. Before, the selection read as
a blank line (`ydisp` NaN after the reset) and the button copied nothing useful.

### @LETTER@2. Headless

1. Run `HEADLESS_UNITS=cleanCopy node scripts/test-board-headless.js`.

**Expected:** passes. Step 1c reattaches while scrolled back and reads the viewport at once, which failed with
`{"top":855,"ydisp":0}` before `resetTerm` synced it.
