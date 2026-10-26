## Test plan

## @LETTER@. A resize repaint does not double or drop history

### @LETTER@1. Widen, then heighten, under a long answer

1. On Windows, ask a Claude Code card for an answer longer than the pane that holds a markdown table.
2. When it is done, widen the pane, then make it taller.
3. Reload the board so the card reattaches, and scroll back over the table.

**Expected:** the table's header row appears once, and every row of the table and the text under it is there.
