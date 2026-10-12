## Test plan

## @LETTER@. The hub remote has one url per worktree

### @LETTER@1. Launch from a second room

1. Share one checkout between two rooms, so its `.git/config` has room A's `remote.hub.url`.
2. Launch a worker from room B into a worktree of it.
3. In the worktree run `git config --get-all remote.hub.url`. In the main checkout run the same.

**Expected:** the worktree prints one url, room B's forwarder. The main checkout prints one url, room A's.

### @LETTER@2. Repair

1. In a worktree that already has two urls, launch again from room B.

**Expected:** `git config --get-all remote.hub.url` prints one url, and `git fetch hub` is not refused.
