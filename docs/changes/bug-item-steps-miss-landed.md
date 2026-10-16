## Test plan

## @LETTER@. Landed items read landed

### @LETTER@1. An item nobody gave a branch

1. Open `/_hub/work` for an item that was launched with no branch and has landed on claude/main and origin's main.
2. Its landed step is done at the commit on claude/main that added its changelog entry, and its pushed step is done.
3. Every step row carries the item's backlog id, none is empty.

### @LETTER@2. A cherry-picked item

1. Pick an item whose builder sha is not on main, because it was cherry-picked.
2. Landed is found by its `changelog/<dept>/<date>-<id>.md` entry. An item whose id is the tail of another's, such as
   `stuck` and `r-stuck`, is not matched by the other's entry.

### @LETTER@3. A bug with no design

1. Find a bug whose builders finished and that no designer was ever on.
2. Designed and approved read skipped with a reason, and the current step is never designed.
