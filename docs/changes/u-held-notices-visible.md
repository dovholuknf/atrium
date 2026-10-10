## Test plan

## @LETTER@. Held notices are visible on the board

Needs a room with a card tagged `atrium:hold-notices`. Before and after pictures are in
`docs/changes/u-held-notices-visible/`.

### @LETTER@1. The icon and the count

1. Tag a launching card `atrium:hold-notices`. Launch two workers from it and have each call `atrium_done`.
2. Open the terminals tab, then the stack and the board.

**Expected:** the launcher's row, and its terminal bar once attached, wear a mailbox icon and `2`. Hovering the row
for a second shows the same icon and a `held notices` line in the details. A card with nothing held shows none.
Each worker's `atrium_done` answered `launcher_told: held`, and its note says the report waits on the launcher's card.

### @LETTER@2. Click to read

1. Click the icon.

**Expected:** a list opens with each notice's worker, its source, how long ago and its text, oldest first. The count
disappears from every surface. `atrium_task` with `notices: true` from the launcher then answers nothing unread.
Dragging across the icon to select text opens nothing.