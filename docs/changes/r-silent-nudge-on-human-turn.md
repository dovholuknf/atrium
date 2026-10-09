## Test plan

## @LETTER@. No silent-stop nudge in a human's conversation

### @LETTER@1. Human turn ends silent

1. Have an agent launch a worker and let it report done, or sit idle after its first turn.
2. Type a question to the worker in the board's message box and let it answer without calling atrium_done or atrium_say.

**Expected:** nothing is typed into the worker afterwards and the launcher is told nothing.

### @LETTER@2. Launcher prompt still nudges

1. Have the launcher say something to the worker that wants work done.
2. Let the worker end its turn without atrium_done, atrium_blocked or atrium_say.

**Expected:** the worker is nudged once, as before.
