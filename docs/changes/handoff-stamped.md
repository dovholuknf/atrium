Docs: none, the handoff file name is not documented

## Test plan

## @LETTER@. Each context cycle writes a new handoff file

### @LETTER@1. Two cycles on one card

1. Start a new context on a card from the board.
2. Let it finish, then start a second one on the same card.

**Expected:** each limit prompt names a different file, `<card-id>-<yyyymmddhhmmss>.md`. The agent writes the second
file without reading the first, and both files are on disk.
