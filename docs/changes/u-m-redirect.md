## Test plan

## @LETTER@. A phone lands on the phone page

### @LETTER@1. The board root and card paths

1. On a phone, open the board root. It lands on `/m/`.
2. Open `/alias/<a>` and `/room/<room>/<name>` on the phone. They land on the same paths under `/m`.
3. Open `/#term=<id>`. It lands on `/m/#term=<id>` and the card opens.
4. A tablet and a desktop browser, narrow or not, stay on the board.

### @LETTER@2. Choosing the desktop board

1. On `/m/` tap "desktop board". The board opens and stays.
2. Open the bell, then "phone view". It lands on `/m/` and the board root sends the phone there again.

### @LETTER@3. A pop-out is left alone

1. A window popped out from the desktop board for a card stays where it is, on any screen size.
