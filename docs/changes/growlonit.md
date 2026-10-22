## Test plan

## @LETTER@. growlOnIt waits for the pop-out's own attach

### @LETTER@1. A focused pop-out silences its card

1. Run `HEADLESS_UNITS=growlOnIt,bootClean node scripts/test-board-headless.js`.

**Expected:** both units pass, and no question is drawn on the board or in the pop-out for a card whose pop-out is focused.
