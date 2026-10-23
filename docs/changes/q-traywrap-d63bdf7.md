## Test plan

## @LETTER@. The tray wrap check measures a settled tray

### @LETTER@1. Slow slide-in

1. Run `HEADLESS_UNITS=notifyTrayWrap node scripts/test-board-headless.js` with the tray's `animation-delay` raised to
   400ms.

**Expected:** the unit passes, because it waits for the tray's animation to end rather than for its transform to read
`none`.
