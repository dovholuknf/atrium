## Test plan

## @LETTER@. Two phone units survive a stalled frame

### @LETTER@1. mOpenQ taps back on a settled sheet

1. Run `HEADLESS_UNITS=mOpenQ node scripts/test-board-headless.js`.
2. To force the stall, after `mPage` in the unit wrap `requestAnimationFrame` in the page so callbacks wait 7 s once
   `#m-card` is shown.

**Expected:** the unit passes. Without the wait for `#m-card.on` and finished transitions, the back tap times out
at 5 s with the sheet at x=412.

### @LETTER@2. mStickBottom times the jump control from the scroll event

1. Run `HEADLESS_UNITS=mStickBottom node scripts/test-board-headless.js`.
2. To force the stall, in `scrolledUp` add a capture `scroll` listener on `window` that stops the event for
   `#m-card-scroll` and re-dispatches it 4.5 s later.

**Expected:** the unit passes. Without the wait for the scroll event it fails with "no jump control" and
`"scrolls":0`.
