## Test plan

## @LETTER@. The launch dialog's repo list, just pick

### @LETTER@1. One list

1. Open the launch dialog on a room with checkouts, with hub repos the room lacks, then press `repo`.

**Expected:** one list, a row per repository (a repo with a main checkout and worktrees is one row), showing the repo
name only. No "on the hub" badge and no clone button. The filter narrows it.

### @LETTER@2. Pick

1. Pick a repo the room has.
2. Pick a repo only the hub has.

**Expected:** the first fills the working directory with its checkout and clones nothing. The second clones from the
hub onto the dialog's room with no further click, and fills the directory.

### @LETTER@3. A repo the hub lacks

1. Type `host/org/repo` or a URL for a repo neither has, and pick the row that appears.
2. Repeat with a repo the forge refuses.

**Expected:** the hub clones it from the forge, then the room clones from the hub, and the directory is filled. A refusal
at either step is a line in the dialog, the panel stays open and the directory is unchanged.
