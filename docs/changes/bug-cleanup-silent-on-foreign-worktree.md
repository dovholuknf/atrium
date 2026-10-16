## Test plan

## @LETTER@. Terminate and clean up on a folder atrium did not create

### @LETTER@1. A foreign worktree

1. Launch a card in a worktree made by something else (a gwt worktree), so the room's ledger holds no row for it.
2. Terminals tab, right click the card, choose terminate and clean up. Repeat from the phone's long press sheet.

**Expected:** a question reads "Nothing here was created by atrium, so nothing is removed", names the folder and branch
that stay, and offers cancel and terminate. Cancel changes nothing. Terminate stops the session and leaves the folder and
branch on disk.

### @LETTER@2. A folder atrium made

1. Do the same on a card whose worktree atrium created.

**Expected:** no extra question, as before (an ask only for changes that exist nowhere else).

Pictures: `docs/changes/img/bug-cleanup-silent-on-foreign-worktree/` (`board-after.png`, `phone-after.png`). Before, there
was no question at all: the menu's help promised a removal and the session was stopped with nothing freed.
