## Test plan

## @LETTER@. The resume spinner unit no longer races its bound

### @LETTER@1. Still spinning before the bound

1. Run `node scripts/test-board-headless.js resumeSpinner` on a loaded machine.

**Expected:** the unit passes. It checks the spinner with the 30 second bound, then restarts it with a 400ms bound and
waits for the "no output after" reason.
