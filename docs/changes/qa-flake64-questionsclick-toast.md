## Test plan

## @LETTER@. questionsClick is not blocked by a leftover toast

### @LETTER@1. The unit runs alone

1. Run `HEADLESS_UNITS=questionsClick node scripts/test-board-headless.js`.

**Expected:** the unit passes, and no click waits on a `.toast` covering the chip.
