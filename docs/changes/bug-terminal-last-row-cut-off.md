Docs: none, a fix that makes the terminal fit the pane it already sits in

## Test plan

## @LETTER@. The last terminal row stays above the hint bar

### @LETTER@1. The pane changes height

1. Open a card's terminal and let it fill with output.
2. Open the details drawer under the terminal, or make the window shorter, then close it or make it taller.

**Expected:** after each change the input line is the last row, fully visible just above the hint bar, never half
hidden behind it. Before: `bug-terminal-last-row-cut-off-before.png`. After: `bug-terminal-last-row-cut-off-after.png`.

### @LETTER@2. A fresh attach

1. Open a card's terminal in a window of any height.

**Expected:** the last row sits wholly above the hint bar, whatever the remainder of the pane's height over a row.
