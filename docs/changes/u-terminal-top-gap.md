## Test plan

## @LETTER@. A terminal starts at the top of its pane

### @LETTER@1. No band above the first line

1. Attach a terminal on the Terminals tab and let it print a few lines.
2. Attach the same card from a second, shorter window, so the pty is held a few rows short of the first window.

**Expected:** in the taller window the first line sits directly under the card header. Any leftover space is below the
last row, above the help line.

### @LETTER@2. A resize keeps it

1. Drag the window taller and shorter by part of a row.

**Expected:** the first line stays against the top of the pane after every resize.
