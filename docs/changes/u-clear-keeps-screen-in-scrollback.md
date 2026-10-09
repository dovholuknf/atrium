## Test plan

## @LETTER@. A cleared session keeps its screen in scrollback

### @LETTER@1. Typed /clear

1. Open a Claude Code card on the board and let it fill a screen with output.
2. Type `/clear` in the terminal and press Enter while the board is showing it.
3. Scroll up.

**Expected:** what was on screen before the clear is above the new banner. Nothing is blank between them.

### @LETTER@2. New context

1. Start a new context from the card menu on a card with a full screen.
2. Wait for the cycle to clear the session.
3. Scroll up in the terminal.

**Expected:** the screen that was showing when the session was cleared is in scrollback.

### @LETTER@3. Reload agrees with live

1. After either clear above, reload the board and open the same card.
2. Scroll up.

**Expected:** the same old screen is in scrollback, once, in the same place.

### @LETTER@4. A full-screen program

1. In a shell card, run a pager or editor and quit it.

**Expected:** nothing it drew is added to scrollback.
