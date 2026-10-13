## Test plan

## @LETTER@. A card's tags survive a relaunch

### @LETTER@1. Interview tag through a relaunch

1. Launch an interview card with `atrium_launch kind: interview`.
2. Launch onto the same card again with other tags, then restart it from the terminal's menu with the full setup.

**Expected:** the card still carries `atrium:interview` and stays in the interview group.

### @LETTER@2. A human can still remove a tag

1. Open the card's tag editor on the board and remove `atrium:interview`.

**Expected:** the tag is gone and stays gone.
