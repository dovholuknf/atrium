Docs: none, a test fix that follows the tray toast rework

## Test plan

## @LETTER@. A toast about nothing leaves the open tray where it is

### @LETTER@1. Click a toast that has no destination while the tray is open

1. Raise a toast with no card and no destination, then open the notification tray.
2. Click the toast, which sits to the left of the tray.

**Expected:** the board stays on the stack and the tray stays open.
