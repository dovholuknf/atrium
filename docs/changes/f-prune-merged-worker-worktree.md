Docs: none, the setting is described in the changelog entry

## Test plan

## @LETTER@. Landed workers prune themselves

### @LETTER@1. A junction is not followed

1. Launch a worker over /v1/launch with lifetime `work`, link the shared `node_modules` into its worktree
   (`mklink /J node_modules <shared>`), let it commit, and land its commit on `claude/main` by cherry-pick.
2. Let the worker finish and its runner exit, then wait `landed_reclaim_grace` (set it to `60`).

**Expected:** the worktree and branch are removed, the card has one `pruned` line, and the shared `node_modules` is intact.

### @LETTER@2. A dirty or unmerged worker is kept

1. Repeat @LETTER@1 with an uncommitted file in the worktree, then with a commit that is not on `claude/main`.

**Expected:** nothing is removed and the card says why, once.
