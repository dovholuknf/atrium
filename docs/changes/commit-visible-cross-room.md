## Test plan

## @LETTER@. A launcher on another room still sees the worker's commits

### @LETTER@1. Launch text for a launcher on another room

1. On room A, in a checkout, start a launcher session. On room B, same machine, launch a worker with a brief into a
   worktree of that same checkout, using `atrium_launch` with `room` set to B.
2. Read the first prompt the worker was given.

**Expected:** it says "Your commits are already visible to <launcher>@A" and not to push. A launcher whose directory is
in a different repository gets no such sentence.

### @LETTER@2. atrium_git_push from that worker

1. Point the shared `hub` remote at room A's forwarder, have the worker commit and call atrium_git_push.

**Expected:** "nothing to push: <launcher>@A sees this commit already", as a success.

### @LETTER@3. An old store and a failing git

1. Start a room on a store from before this change, then launch across rooms from a launcher whose directory is gone.

**Expected:** the store opens (migration 0096 adds the column once and tolerates it being there) and the worker's
prompt carries no visibility line and no error.
