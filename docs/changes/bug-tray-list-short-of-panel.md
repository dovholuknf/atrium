Docs: none, a fix that makes the tray match what it already says

## Test plan

## @LETTER@. The notification tray fills its panel

### @LETTER@1. List reaches the bottom edge

1. Raise enough notifications that the bell's tray list scrolls.
2. Open the tray in a short window and again in a tall one.
3. Scroll the list to its end.

**Expected:** the panel starts at the top of the window and ends at its bottom. The list and its scrollbar end at the
panel's bottom edge with no blank panel below, and the oldest row scrolls fully into view.
