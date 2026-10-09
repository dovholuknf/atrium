## Test plan

## @LETTER@. A cleared context leaves a line

### @LETTER@1. Live clear

1. Open a supervised terminal with some output and type `/clear`.

**Expected:** a divider reading "context cleared" and the local time sits between the kept page and the new screen.

### @LETTER@2. Reload

1. Reload the board and open the same terminal.

**Expected:** the divider is in the same place, once, and reads "context cleared" with no time.

### @LETTER@3. Several clears

1. Clear twice more.

**Expected:** each clear has its own divider. Selecting and copying rows around a divider returns only terminal text.
