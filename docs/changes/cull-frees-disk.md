## Test plan

## @LETTER@. A cull frees the disk and says how much

### @LETTER@1. A worktree with an ignored deep folder

1. Make a merged worker's worktree with an ignored `node_modules` holding a path past 300 characters and a 100 KB file.
2. Call `atrium_cull` on the worker.

**Expected:** the worktree folder is gone from disk, `bytes_freed` is at least 100000, and the tool text names the
number.

### @LETTER@2. A worktree that cannot be fully removed

1. Hold a file open under the worktree, then call `atrium_cull`.

**Expected:** `kept` says what is left on disk, `worktree_removed` is false, `bytes_freed` is 0, and the branch is kept.

### @LETTER@3. Close is unchanged

1. Close a card whose worktree holds an ignored deep folder.

**Expected:** the folder is gone and the worktree row counts its bytes, as before.
