## Test plan

## @LETTER@. A ready alert rings once per wait

### @LETTER@1. A wait rings once, after the card goes quiet

With the board open and unmuted, let a card finish its turn. Nothing rings at once. About 5 seconds later one
"is ready" alert rings. Open the toast log: the card appears once.

### @LETTER@2. A busy card does not repeat it

Give a card short turns that wake again (a background task or a message). Each new wait may ring once, never twice,
and a wait that ends inside 5 seconds never rings. Run `HEADLESS_ONLY=readyOnce node scripts/test-board-headless.js`.

### @LETTER@3. A window reading the card says nothing

Pop a card out and focus that window. When the card finishes its turn there is no toast, only a toast log line.
Click another window: the pop-out now rings. The board with that card's terminal showing behaves the same.
Terminal output inside the 5 seconds delays the ring. Run
`HEADLESS_ONLY=readyPopout node scripts/test-board-headless.js`.
