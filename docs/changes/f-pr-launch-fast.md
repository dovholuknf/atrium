## Test plan

This is item 4 of u-paste-a-link-fixes: Enter in the paste box launches with no dialog. It also makes the open itself
faster and says what it is doing.

## @LETTER@. Pasting a pull request link opens fast

### @LETTER@1. Enter opens with no dialog

1. Copy a pull request link and press Ctrl-Alt-R.
2. Wait for the box to read the link, then press Enter.

**Expected:** the launch dialog never appears. A line over the board names each step as it runs (reading the pull
request, finding the clone, fetching the head, making the worktree), then goes when the card is attached.

### @LETTER@2. A failed open says why

1. Paste a link whose head cannot be fetched, then press Enter.

**Expected:** the launch dialog opens with the reason in its link note.

### @LETTER@3. Shift-Enter and plain paste

1. Press Shift-Enter in the box on a link.
2. Close it, then paste a link onto the board with Ctrl-V.

**Expected:** Shift-Enter stops at the filled-in launch dialog with nothing started. The plain paste opens the box
with the link and not the launch dialog.

### @LETTER@4. Timings in the log

1. Open a pull request link, then read the room log.

**Expected:** each step has a line like `[atrium open] <url>: <step> took Ns`.
