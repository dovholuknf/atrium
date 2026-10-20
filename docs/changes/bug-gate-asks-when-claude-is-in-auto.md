## Test plan

## @LETTER@. Board-wide auto mode defaults on

### @LETTER@1. A room that never wrote the switch

1. Use a room whose `global_auto` setting was never written, with atrium's hooks installed.
2. Open the board and run a tool call in a joined session.

**Expected:** the header reads "approving everything" and the call is approved at once, recorded as `global-auto`.

### @LETTER@2. An explicit off asks

1. Turn the board-wide switch off from the header.
2. Run a tool call in a joined session.

**Expected:** the card moves to needs-permission and waits for a decision. A restart keeps it off.

### @LETTER@3. Rules still win

1. With the switch on, write a `never` rule for a command and run it.

**Expected:** the call is blocked by the rule.
