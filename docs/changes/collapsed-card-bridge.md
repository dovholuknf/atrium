## Test plan

## @LETTER@. A collapsed parent carries the bridge to the attached child

### @LETTER@1. One level down

1. In Terminals, turn the subagents chip on and attach a card that a parent launched.
2. Collapse the parent with its caret.

**Expected:** the parent row wears the attached card's frame and a funnel runs from it to the pane. The parent stays
collapsed and no other row is opened.

### @LETTER@2. Two deep and expanding

1. Attach a grandchild, then collapse its parent and the grandparent.
2. Expand the grandparent, then the parent.

**Expected:** collapsed, the outermost visible row carries the funnel. Expanding hands it down, and with the attached
card's own row showing it carries the ordinary bridge.

### @LETTER@3. A shut group around it

1. Put the collapsed parent in a group and shut the group.

**Expected:** the group heading carries the funnel, as before.
