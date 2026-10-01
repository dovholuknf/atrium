## Test plan

## @LETTER@. The phone home order, a way out of a card address, and the message box

### @LETTER@1. The /m home is newest first

1. Open `/m/` with two or more cards of different ages. The list is newest activity first, in "needs you" and in "all".
2. Tap "view". Choose "oldest". The list reverses.
3. Choose group "project", then "room". Headings appear, alphabetical, with the order kept inside each.
4. Switch on "hide subagents", "hide finished" and "needs me only". Each removes what it names from "all".
5. Reload the page. Every choice is still set.

### @LETTER@2. A card opened by its address can be left

1. From the board, follow a link to `/alias/<a>`. The terminal bar shows "all cards" and "cards". Tap "all cards". The board opens.
2. Tap "cards". The card picker opens. Pick another card and it opens in the same window.
3. Open `/alias/<a>` from the board and press the browser Back button. The board is back.
4. On a phone the tray handle opens the terminal bar and "all cards" floats at the top left while it is shut.
5. On `/m/alias/<a>` the top left says "all cards" and leaves for the list. "cards" lists the other cards and a tap opens one.

### @LETTER@3. The phone board boots cleanly

1. Load `/`, `/#term=<id>` and `/alias/<a>` on a phone at 390 and 412 wide.
2. `/` shows the header. The other two show the way out, the picker opens and switching lands on the other card.
3. The console shows no uncaught error. Screenshot: `docs/backlog/ui/img/u-m-home/`.

### @LETTER@4. Enter sends

1. On a desktop browser, type in the "Message this session" box and press Enter. The message is sent.
2. Press Shift+Enter. A newline is made and nothing is sent.
3. On a touch-only phone Enter still makes a newline and the arrow sends.

### @LETTER@5. The send arrow is centred

1. The arrow sits in the middle of its circle on the phone page and in the board's composer.
2. Before and after crops: `docs/backlog/ui/img/u-m-home/send-arrow-{desktop,phone}-{before,after}.jpg`.
