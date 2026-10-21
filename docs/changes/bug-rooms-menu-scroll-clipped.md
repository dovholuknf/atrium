Docs: none, a fix that makes the menu match what it already says

## Test plan

## @LETTER@. The rooms menu scrolls to its last tile

### @LETTER@1. Last tile whole at the end of the scroll

1. Attach enough rooms that the "N/N rooms" dropdown is taller than the window, or open it at 1280x700 with `?demo=rooms`.
2. Open the menu with fewer rooms, then let more attach while it is open.
3. Scroll the menu to its end.

**Expected:** the last tile is whole, with a small gap between its bottom edge and the window's bottom edge. The same
holds at phone width.
