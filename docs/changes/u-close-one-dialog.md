Covers u-close-one-dialog and u-close-progress. Entry: `changelog/ui/2026-10-10-u-close-one-dialog.md`.

Before: `docs/changes/img/close-dialog-1-before.png` and `close-dialog-2-before.png` (two dialogs).
After: `close-dialog-after.png`, `close-progress-after.png`, `close-failed-after.png`.

How it works: `closeCardNow` in `js/sharing.js` builds one `askUser` dialog. The button has a `hold` function
(`js/browser-dialogs.js`) so the dialog stays up while it runs. `POST /v1/tasks/{id}/close` and `/v1/sweep/close` take
`progress`, an id, and `closeCard` broadcasts a `close-progress` event per step (`internal/api/closeprogress.go`, the
same pattern as `openprogress.go`) and logs each step's seconds. The preview measures each worktree now, so the size
includes ignored trees.

## Test plan

## @LETTER@. Closing a card is one dialog with progress

### @LETTER@1. One dialog, worktree row inline

1. Open a pull request link so its worktree exists, then add a file to the worktree and put a large folder in it.
2. Close the card from its menu.

**Expected:** one dialog. It lists what is freed with the worktree's real size, the large folder included. The
worktree is a row with keep, stash to the hub and delete, and stash is selected. There is no second dialog.

### @LETTER@2. Progress while it runs

1. Press close it.

**Expected:** the dialog stays up, the button is disabled with a spinner, and each step appears in order (stopping the
session, the review, the worktree with its size, branches, refs) and ticks off. The card reads "closing" on the board.
The dialog shuts itself when the close is done.

### @LETTER@3. A failure stays on its row

1. Close a card whose worktree cannot be removed, for example one a program still holds open.

**Expected:** the dialog stays open, the failed row shows what git said, and the only button is dismiss.

### @LETTER@4. The leftovers sweep

1. In pulls, open the leftovers and press close on one group.

**Expected:** the same dialog and the same steps.
