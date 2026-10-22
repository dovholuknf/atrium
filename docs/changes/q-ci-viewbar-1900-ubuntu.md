Docs: none, a CSS tightening and a test fix

## Test plan

## @LETTER@. The view bar stays on the line on a wide window with a wide font

### @LETTER@1. Open the terminals view at 2360px with a wide font

1. Open the board at 2360px wide on a machine whose sans font is DejaVu Sans.
2. Open the terminals view.

**Expected:** SHOW, SORT BY, GROUP and LABEL sit on the search box's line with no caret. Narrowing the window a little
first shrinks the search box (not below 160px) before the controls move into the drawer.
