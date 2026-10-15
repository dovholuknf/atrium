## Test plan

## @LETTER@. Every worktree atrium makes is scoped to this room

### @LETTER@1. A worktree made from the board

1. On a machine where another room's forwarder is the `hub` url in the shared checkout, make a worktree through the
   board (a pull request, an issue link, a branch link, or the provider worktree call).
2. In the new worktree run `git config --worktree --get remote.hub.url`.

**Expected:** it is this room's agent listener plus the `/git/hub/<repo>.git` path, and `git fetch hub` from a session
there is not blocked by the gate.
