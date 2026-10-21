Docs: none, a test helper fix

## Test plan

## @LETTER@. The attach helper under load

### @LETTER@1. Say after a restart

1. Run `TestASayAfterARestartIsTypedIntoTheTerminalTheBoardAttachesTo` with `-tags integration -count=3`.

**Expected:** it passes every time, including on a loaded machine.
