## Test plan

## @LETTER@. A terminal reset survives a pending scroll event

### @LETTER@1. cleanCopy resets through resetTerm

1. Run `HEADLESS_UNITS=cleanCopy node scripts/test-board-headless.js`.
2. To force the race, after the attach in the unit loop 300 times: write 60 lines, `term.scrollLines(-5)`, wait a few
   milliseconds, call `term.reset()` in place of `resetTerm(term)`, wait a few more, and read
   `term._core.buffer.ydisp`.

**Expected:** the unit passes. With a raw `term.reset()` in the loop `ydisp` turns NaN within a few hundred rounds
and the page throws `Cannot read properties of undefined (reading 'loadCell')` from the WebGL renderer's `_updateModel`.

### @LETTER@2. A reattach while scrolled back

1. Open a card terminal, scroll it up, and stop and start the room so the socket reattaches.

**Expected:** the screen repaints from the replay and scrolling still works. Before, a scroll event landing between the
reset and xterm's next frame could blank the terminal until the next scroll.
