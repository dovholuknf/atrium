## Test plan

## @LETTER@. A cull and a close read their git refs once

### @LETTER@1. A merged worker is culled

1. Merge a worker's branch into `claude/main`, then let its cull time come (or run `atrium_cull`).
2. Check the card is archived, the worktree is gone and the branch is deleted.

**Expected:** all three happen, and the room log shows no `no longer reads as merged` line.

### @LETTER@2. A branch that moved is kept

1. Cull a merged worker whose branch gains an unmerged commit just before the branch is deleted.

**Expected:** the worktree is gone, the branch is kept, and the answer says it no longer reads as merged.

### @LETTER@3. A dirty worktree is kept

1. Leave an uncommitted file in a merged worker's worktree and cull it.

**Expected:** the worker is asked to leave, and the worktree and branch stay with the changed files listed.

### @LETTER@4. Closing a card with hub branches

1. Close a card holding one merged and one unmerged hub branch.

**Expected:** the unmerged branch is asked about, the merged one is deleted on the hub once confirmed, and asking twice
gives the same answer each time.
