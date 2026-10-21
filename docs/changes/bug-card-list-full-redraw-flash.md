Docs: none, a fix that makes the list do what it already claimed

## Test plan

## @LETTER@. The Terminals list patches rows instead of redrawing

### @LETTER@1. One card changes

1. Open the Terminals tab with a dozen or more cards, sorted by name.
2. Let one card start and stop a tool.

**Expected:** only that card's row changes. No other row flickers, and the spinners on working rows do not restart.

### @LETTER@2. A card moves under sort by activity

1. Switch the sort to activity and let the top working card go idle.

**Expected:** that row slides down alone. The rows it passes keep their place and their running animation.

### @LETTER@3. Dragging still works

1. Drag a row into the pinned bucket, and drag a row to reorder inside it.

**Expected:** both still work after several updates have arrived.
