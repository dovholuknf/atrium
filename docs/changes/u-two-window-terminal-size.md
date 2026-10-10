## Test plan

## @LETTER@. A terminal in two board windows follows the one used last

### @LETTER@1. Size follows focus

1. Open a card's terminal in one board window, then open the same terminal in a second window of a different size.
2. Click into the second window, then click into the first.

**Expected:** the terminal is sized for the window you clicked last each time, and there is no empty band above the first
line of the first window.

### @LETTER@2. Size returns when a window leaves

1. With both windows showing the terminal, click into the second window and then close it, or switch it to another card.

**Expected:** the terminal returns to the first window's size without any action there.
