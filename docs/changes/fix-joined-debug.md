## Test plan

## @LETTER@. The details drawer has a debug section

### @LETTER@1. Section only on the strip
Attach a terminal and open `details` on its shortcut strip. A `debug` section sits under the numbers with the live
gate, chars on the line, last key and messages held with who sent them. Hover a card for a second: its details have no
debug section.

### @LETTER@2. Countdown
Type a character and delete it, then watch the gate line. It reads `gate closed, opens in 2s` and counts down to open.

### @LETTER@3. Gate line only when blocking
Tick `show the typing gate line`. With nothing held the line under the terminal stays hidden. With a message held
behind your line it reads `1 message from @name waits: N chars on your line`, without a copy of the line.

### @LETTER@4. Outside click and Escape
Open `details`. A click inside it keeps it open. A click anywhere else, including in the terminal, closes it and the
terminal keeps the focus. Open it again and press Escape: it closes and the program does not see the key.

### @LETTER@5. Every window follows
Open a second window of the board. Tick `log terminal input lag` or the gate line in the first. The same box is ticked
in the second with no reload, and the console in it starts or stops logging. The settings dialog no longer has these
two boxes and points at the drawer.
