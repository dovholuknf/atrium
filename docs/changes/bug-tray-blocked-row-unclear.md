## Test plan

## @LETTER@. A blocked tray row says what to do and resolves

### @LETTER@1. Reading a blocked row

1. Launch a card that stops at Claude Code's folder-trust prompt and open the tray.

**Expected:** the row says Claude Code is asking whether to trust the folder, that atrium cannot answer it, and to open
the terminal and choose trust or exit the card.

### @LETTER@2. A row after the card moved on

1. Exit that card (or answer the prompt) and open the tray again, then click its old blocked row.

**Expected:** the row is dimmed and reads "resolved: the card has exited" (or "it is running now"). For an exited card
the click says so in the row and goes nowhere. A new row arriving while the tray is open has a wide teal left edge.
