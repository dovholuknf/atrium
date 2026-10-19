## Test plan

## @LETTER@. A landed worker is reclaimed

### @LETTER@1. A rebased branch is landed

1. Launch a worker, let it commit in its worktree, then cherry-pick its commit onto `claude/main` so the sha differs.
2. Call `atrium_cull` on the worker.

**Expected:** the worker is not refused as unmerged. Its worktree is removed and its branch deleted.

### @LETTER@2. An idle landed worker goes after the grace

1. With `merged_cull_grace` unset (a day), leave a landed worker at needs-input with a clean worktree. Set the setting to
   `60` to shorten the wait.
2. Wait a minute and a reaper tick.

**Expected:** the worker's runner exits, its worktree and branch are removed, and its card leaves the board.

### @LETTER@3. A kept one says why

1. Repeat @LETTER@2 with a worker tagged `atrium:keep-open`, one held with `atrium_cull hold=true`, and one with an
   uncommitted file in its worktree.

**Expected:** none is removed. Each Terminals row shows a `landed` chip whose tip names the reason.

### @LETTER@4. The operator's cards are untouched

1. Start a card by hand in a worktree whose branch has landed and leave it at needs-input past the grace.

**Expected:** nothing happens to it.

### @LETTER@5. Close finds the worktree

1. Close a launched worker whose inventory has no worktree row.

**Expected:** the preview lists its worktree and branch, and confirming removes them. A dirty one is asked about.
