## Test plan

## @LETTER@. View controls inline and the alias label

### @LETTER@1. The controls sit on the line when they fit

1. Open the terminals tab in a window about 1500 px wide.
2. Look at the line under the title bar.

**Expected:** show, sort by, group and label are on the line beside the search box, with no summary text and no caret.
Pressing a pill applies at once and no drawer opens.

### @LETTER@2. The drawer comes back when the row would wrap

1. Narrow the window to about 900 px, then to a phone width.
2. Click the line.

**Expected:** the summary and the caret return and a click opens the drawer holding all four rows. Widening the window
again puts the row back on the line.

### @LETTER@3. Label by alias

1. Give two cards an alias and leave a third without one.
2. On the terminals tab choose `alias` in the label control.

**Expected:** the two cards lead with their alias and the third with its title. Hovering an aliased row shows its title.
The stack and the board still say `title`, and choosing `alias` on one of them leaves the others alone. Reload and each
tab keeps its choice.
