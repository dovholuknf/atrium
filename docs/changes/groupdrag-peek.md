## Test plan

## @LETTER@. A hover peek never blocks a drag a person starts

### @LETTER@1. Rest on a card, then drag another

1. Rest the pointer on a terminals row for over a second until its peek opens.
2. Press on the row under the peek's edge, or move off the peek and press on another row, and drag it to a group.

**Expected:** pressing closes the peek and the drag starts. The headless unit `groupDrag` passes.
