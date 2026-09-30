## Test plan

## @LETTER@. A pop-out's bell is its own

### @LETTER@1. The drawer's toggle is the card's

Pop a card out. In the pop-out, open the bell drawer and click "turn off". The drawer says "off for this window". On
the main board the bell stays on, and the board still toasts for other cards. Close the pop-out and open it again:
it is still off.

### @LETTER@2. The board-wide switch still wins

Turn notifications off on the main board. The pop-out stops toasting and sounding too, its drawer says "off for the
whole board, change it on the board", and its toggle is disabled. Everything is still in the toast log.

### @LETTER@3. Mute in a pop-out mutes that card

Click the sound button in the pop-out. The pop-out goes quiet and the board's sound button stays on. The gear in a
pop-out has no notifications section. Close the pop-out and the board announces the card under its own settings.
Run `HEADLESS_ONLY=popoutNotify node scripts/test-board-headless.js`.
