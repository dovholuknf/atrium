## Test plan

## @LETTER@. The launch dialog's repo picker

### @LETTER@1. Checkouts and hub repos

1. Open the launch dialog on a room that has run cards, then press `repo`.
2. Type in the filter box.

**Expected:** the list paints at once. This room's checkouts show first with repo, path and branch, newest use first,
then the hub's repos the room has no checkout of. The filter narrows both. Choosing a checkout fills the working
directory. No request to `/v1/providers` is made.

### @LETTER@2. Clone from hub

1. Press "clone from hub" on a hub repo with no checkout on the selected room.

**Expected:** the room clones from the hub mirror, never a forge, into `<git.scm_root or ~/git>/<host>/<org>/<repo>`
(host is `github`, not `github.com`), and the working directory is filled. With no hub the room says so in the dialog.

### @LETTER@3. Other rooms

1. Change the dialog's ROOM select.

**Expected:** the list is that room's checkouts, and a clone lands on that room.

### @LETTER@4. Text selection

1. Drag-select inside each input, then drag from an input out onto the backdrop. Double-click a word, shift-click,
   and select label and hint text.

**Expected:** the dialog stays open and each selection holds.

Providers are unchanged: the runners > providers pane, its API and PR/branch worktrees still work. The old pickrepo and
pickwhere dialogs, and "make worktree" inside the picker, are gone.
