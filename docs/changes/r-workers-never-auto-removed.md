## Test plan

## @LETTER@. A merged worker stays

### @LETTER@1. A new room does not cull on merge

1. On a room where `merged_cull_grace` was never set, or was cleared in settings, let a launched worker report done and
   merge its branch into `claude/main`.
2. Wait past 30 minutes.

**Expected:** the worker's card, worktree and branch are all still there. Settings shows `merged_cull_grace` empty and
`merged_cull_grace_default` as `off`.

### @LETTER@2. Explicit tools still remove

1. Call `atrium_cull` on that worker, or `atrium_exit`.

**Expected:** it is reclaimed or exits, as before.
