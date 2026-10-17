Docs: none, a test fix.

## What and why

Run 39052523653, macos board-1: `pasteSpinner` failed in the shard and again alone with `a paste echoed once and then
quiet never cleared`. Its second case sends a one byte paste and, 60 ms later, has the mock socket echo it back, then
waits up to 4 s for the flight to end. `sendPaste` sends on the next frame (or at 100 ms, whichever is first), and
`pasteSawOutput` ignores output that arrives before the bytes have left: output before the send says nothing about
it. On the loaded macos runner the frame came after 60 ms, so the echo was dropped, the flight waited for an echo
that never came, and only the 20 s cap would have ended it.

The echo now waits for the flight to report its bytes sent, polling every 10 ms from the 60 ms mark, and the "held"
read is taken right after the echo rather than at a fixed 150 ms, which is the moment the case is about. 15 of 15
under six CPU burners in a Linux container.

## Test plan

## @LETTER@. The quick paste case waits for the send

### @LETTER@1. Under load

1. Start CPU burners.
2. `HEADLESS_UNITS=pasteSpinner node scripts/test-board-headless.js` several times.

**Expected:** passes every time, and the quick case's echo lands after the send.
