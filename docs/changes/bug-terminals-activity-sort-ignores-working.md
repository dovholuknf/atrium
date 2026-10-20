Docs: none, a fix that makes the sort do what its tooltip already says

## Test plan

## @LETTER@. Terminals sort by activity puts a working card first in a group

### @LETTER@1. Working card leads its own group

1. Open the Terminals tab, set the gear's sort to `activity` and grouping to `by group` with a group that holds
   several cards.
2. Let one idle card in that group start working.

**Expected:** that card moves to the top of the group, the rest follow by most recent activity.

### @LETTER@2. Other sorts keep hand order

1. Set the sort to `name`.

**Expected:** cards in your own groups are in the order you dragged them.
