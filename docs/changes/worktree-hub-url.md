## Test plan

## @LETTER@. A worktree's hub remote is the launching room's

### @LETTER@1. Two rooms, one checkout

1. In a checkout whose `hub` url points at room A's forwarder, `git worktree add` a worktree.
2. Launch a worker from room B in that worktree.
3. Run `git -C <worktree> config remote.hub.url` and the same in the main checkout.

**Expected:** the worktree shows room B's forwarder with the same `/git/hub/...` path, the main checkout still shows
room A's, and `git push hub <branch>` from the worker is allowed.

### @LETTER@2. Nothing to scope

1. Launch a worker in a directory that is not a git repo, then in a repo with no `hub` remote.

**Expected:** both launch normally and no git config is changed.
