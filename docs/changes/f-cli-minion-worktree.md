## Test plan

## @LETTER@. atrium worktree makes, lists and removes claude/ worktrees

### @LETTER@1. New and list

1. In a checkout with a `claude/main` branch, run `atrium worktree new demo`.
2. Run `atrium worktree list`.

**Expected:** branch `claude/demo` exists off `claude/main`, its worktree is listed with 0 ahead and 0 behind.

### @LETTER@2. Remove

1. Run `atrium worktree rm demo`.

**Expected:** the worktree is gone from `git worktree list`, and the merged branch `claude/demo` is deleted.
