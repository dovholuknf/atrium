The phoneNudge unit asserted the header bell's envelope after several awaits. A terminal list refresh (`renderTermList`
calls `followHeldLine` with the live card, which holds nothing) can land in that gap and set the held count to 0, which
hides the envelope. The unit now sets the held count and reads the envelope in one page evaluate, so nothing runs
between them.

## Test plan

## @LETTER@. phoneNudge holds under a late list refresh

### @LETTER@1. Run the unit

1. Run `HEADLESS_UNITS=phoneNudge node scripts/test-board-headless.js`.

**Expected:** the line "the units asked for passed: phoneNudge" and a TIMING line.
