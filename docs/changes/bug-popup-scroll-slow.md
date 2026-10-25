Docs: none, a fix with nothing a reader can name

## Test plan

## @LETTER@. A dialog scrolls at frame rate

### @LETTER@1. Wheel through controls used

1. Open the top menu and choose controls used, on a board with enough history that the list scrolls.
2. Record a Performance trace in DevTools while scrolling the list up and down with the wheel.

**Expected:** the board behind is dimmed, not blurred, and the frames stay under 16 ms. The same holds for settings
and any other board dialog.

### @LETTER@2. A card's label keeps its separator

1. Click a card's title on the board, then open controls used.

**Expected:** a label made of a title and a cwd reads `title cwd` with a space between them, not run together.
