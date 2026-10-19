## Test plan

## @LETTER@. mTypeSteady waits for the thread to settle

### @LETTER@1. Emptied box follows a new reply

1. Run `HEADLESS_UNITS=mTypeSteady node scripts/test-board-headless.js`.

**Expected:** the unit passes, and on a slow machine it reads the scroll gap only once the thread has followed the new
reply or a 3 second deadline has passed.
