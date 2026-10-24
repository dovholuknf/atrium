## Test plan

## @LETTER@. Landing is read without a git process per item

### @LETTER@1. A quiet hub runs no lifecycle git

1. Run a hub with twenty or more open backlog items, some landed on claude/main.
2. Wait for two lifecycle passes (a minute) with no push or mirror move, and watch its git.exe children.

**Expected:** no `git log ... -- changelog/*/*-<item>.md` children appear, and the items' landed and pushed steps are
unchanged.

### @LETTER@2. A landing is still seen

1. Land an item's changelog entry on claude/main.
2. Open the item on the board.

**Expected:** its landed step is done with the commit that added the entry.
