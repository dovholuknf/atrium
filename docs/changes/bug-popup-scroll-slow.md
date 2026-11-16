Docs: none, a fix with nothing a reader can name

## Test plan

## @LETTER@. A dialog scrolls at frame rate

### @LETTER@1. Wheel through settings

1. Open settings, on a window short enough that it scrolls.
2. Record a Performance trace in DevTools while scrolling it up and down with the wheel.

**Expected:** the board behind is dimmed, not blurred, and the frames stay under 16 ms. The same holds for any other board
dialog.
