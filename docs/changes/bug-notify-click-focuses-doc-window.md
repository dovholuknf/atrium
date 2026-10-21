Docs: none, a fix that makes the click match what it already says

## Test plan

## @LETTER@. A notification click lands on the board

### @LETTER@1. A doc window is open beside the board

1. Open a doc page (read.html) and the board in separate windows.
2. Click an OS notification.

**Expected:** the board window comes forward and goes to the alert. The doc window stays where it was.

### @LETTER@2. Only a doc window is open

1. Close the board and keep a doc window open.
2. Click an OS notification.

**Expected:** a new board window opens carrying the alert.
