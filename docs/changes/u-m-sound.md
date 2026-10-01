## Test plan

## @LETTER@. Sound and the bell on a phone

### @LETTER@1. The phone plays sounds

1. Load the board on a phone. While the browser keeps sound locked a "tap to enable sound" pill shows near the top.
2. Touch the screen once. The pill goes and a ready or permission alert plays its tone.
3. Lock the screen or switch app, then have a card finish. The alert is not taken for one you are already reading.

### @LETTER@2. The bell on /m

1. Open `/m/`. The header has a bell. A new permission request or a card that starts waiting plays a tone and adds to its count.
2. Tap the bell. The log lists what arrived and the count clears. A row opens its card.
3. Tap "sound off" in the log. The page stays quiet, and the board on the same browser is muted too.

### @LETTER@3. The bell on the phone board

1. On the board at phone width, in the card list, a `#term=` window and a `/alias/` window, a bell is on screen with its count.
2. Open it. "sound on" and "sound off" in the drawer flip the same switch the header's sound button does.
