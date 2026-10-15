## Test plan

## @LETTER@. A held link goes to its card

### @LETTER@1. The board

1. On two rooms, open an issue link on room A and leave its card running.
2. Pick room B by name is NOT used. Paste the same link from room B's board with the room set to B and no pick.
3. The failed card reads "that link is held by A" and its first button is `go to card`. Click it: A's card comes up
   in the terminals view.

### @LETTER@2. The CLI

1. `atrium open <the same link> --room B` prints the refusal sentence, the card and its board address, and exits 0.
2. Add `--attach`: this terminal attaches to A's card. Ctrl+] leaves it.
3. With the holder's card ended, the same command still fails with "that link is held by A".
