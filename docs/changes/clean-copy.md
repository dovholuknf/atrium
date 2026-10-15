Copying from a terminal goes through `copySelection` in `js/terminal.js`, and so through `cleanCopy`. It reads the
card's last ten replies from `GET /v1/tasks/{id}/replies`, waits at most 800 ms, and looks for the selection in them
with bars, gutters, markdown markers and whitespace runs ignored on both sides. On a match the clipboard gets that
reply's own substring. Anything else, including a slow or failing endpoint and a card read off the screen, copies
`getSelection()` exactly as before.

The rules: a selection under 10 normalized characters never matches. The newest reply holding it wins, and a selection
found twice in one reply is ambiguous and falls back. When the selection covers its first rendered row from its text to
the edge, the source line's leading markers (`> `, `- `, `## `) come with it, and likewise the closing markers when it
covers the last row to the edge. A row that starts mid-paragraph is not a source line start, so nothing is added.

## Test plan

## @LETTER@. Copy a reply as written

### @LETTER@1. A quoted block

1. Have Claude write a reply holding a quoted block that the terminal wraps over two rows.
2. Select both rows with the mouse and press ctrl-c, then paste into an editor.

**Expected:** the paste is the source text, one line per source line, with the `>` and no indent or `▎`. A toast reads
"copied as written".

### @LETTER@2. Not a reply

1. Run `ls -la` in the shell, select some of its output and copy it.

**Expected:** the paste is what the terminal showed, and no "copied as written" toast.

### @LETTER@3. The endpoint is down

1. Stop the room's replies route (or copy on a card with no transcript) and copy a selection.

**Expected:** the copy still lands, unchanged, with the usual "copied" toast.
