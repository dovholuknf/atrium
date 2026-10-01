## Test plan

## @LETTER@. The terminals list box draws its hide controls

### @LETTER@1. Box is never an empty frame
Open the terminals view at full list width. The box above the rows holds the `agents` and `subagents` pills at its
left and the `<<` arrow at its right.

### @LETTER@2. Pills hide and count
Press `agents`. Exited sessions leave the list, a joined or supervised live one stays, and the pill reads
`agents (N)` with N the number hidden. Press it again to bring them back.

### @LETTER@3. Shrunk list
Press `<<` until the list shows names only. The pills are gone there and return at full width.
