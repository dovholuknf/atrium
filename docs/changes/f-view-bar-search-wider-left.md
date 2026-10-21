## Test plan

## @LETTER@. The view bar's search box

### @LETTER@1. Wide window

1. Open the terminals, stack and board tabs in a window about 1900px wide.

**Expected:** the controls share the line and the search box follows LABEL directly, wide enough to read its whole
placeholder, with free space after it and not before it.

### @LETTER@2. Drawer

1. Narrow the window to about 900px so the controls fold into the drawer.

**Expected:** the search box sits beside the summary and caret on the left half of the line, with its placeholder
readable. At phone width it still fills the line.

Screenshots: `docs/changes/img/f-view-bar-search-wider-left/`.
