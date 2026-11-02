Docs: none, website/docs/how-it-works.md and docs/runtime/hooks.md are updated on the branch

## Test plan

## @LETTER@. Hooks fail fast and open with atrium stopped

### @LETTER@1. Atrium stopped

1. Install atrium's hooks from the board, then stop the hub and the room.
2. Start `claude` by hand in a plain terminal, send a prompt that runs one shell command, and exit.

**Expected:** the session starts, runs the command and exits. Each atrium hook shows one message within 2 seconds
naming the hook command, the address it tried and "Start atrium with `atrium run`". The model can quote it.

### @LETTER@2. Atrium hung

1. Point the location file at a port that accepts connections and never answers.
2. Repeat @LETTER@1.

**Expected:** the same, with "no answer in time", and no hook takes more than 2 seconds.
