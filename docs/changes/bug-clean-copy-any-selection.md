## Test plan

## @LETTER@. Copy clean on any selection

### @LETTER@1. Tool output and short selections

1. In a terminal card, select a few lines of tool output, then a three-character piece of a line.

**Expected:** the "copy clean" button appears beside each selection at once. Clicking it copies the text without
gutters or bars, with TUI-wrapped lines joined and list items kept on their own lines.

### @LETTER@2. A reply still copies as markdown

1. Select a quoted block from a reply and wait a moment.
2. Click "copy clean".

**Expected:** the reply's raw markdown is copied. Ctrl-c still copies exactly the selection.
