Docs: none, a fix that makes the tray match what it already says

## Test plan

## @LETTER@. The notification tray opens under the header

### @LETTER@1. Placement and height

1. Raise enough notifications that the tray list scrolls.
2. Open the tray from the bell in a short window and again in a tall one.

**Expected:** the header and the bell stay visible above the tray. The tray is never taller than 90% of the window or
past its bottom edge, and the list fills the panel to the bottom with the oldest row scrolling fully into view.
