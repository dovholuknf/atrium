Docs: none, the reference pages do not describe a hand order inside a terminals group

## Test plan

## @LETTER@. Terminals: a drag into a group follows the sort

### @LETTER@1. Drop places the card by the sort

1. Open the Terminals tab, set grouping to `by group` with a group that holds several cards, and sort by `name`.
2. Drag an untagged card into that group and drop it at the top.

**Expected:** the card joins the group and sits where its name sorts, not where it was dropped.

### @LETTER@2. Every sort orders the group

1. Switch the sort to `activity`, then to `started`.

**Expected:** the group's rows follow each sort. Dragging a row within its group does not reorder it.
