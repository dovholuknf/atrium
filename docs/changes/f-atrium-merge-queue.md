Docs: d-merge-queue

## Test plan

## @LETTER@. The merge queue

### @LETTER@1. A clean branch lands

1. In a worker's worktree, commit on `claude/mq-a`, `git push hub claude/mq-a`, then run `atrium merge`.
2. Run `atrium merge --list`.

**Expected:** the request goes queued, merging, testing, then landed with a sha. `claude/main` on the hub is that sha,
and `claude/mq-a` is gone from the hub and, once no worktree has it checked out, from the room. The requester and its
launcher hear merged with the sha.

### @LETTER@2. A red branch goes back

1. Push a branch whose change fails a test in `scripts/test-touched.sh` and run `atrium merge`.

**Expected:** the request is red with the failing test named, `claude/main` did not move, and the requester hears it.
The Terminals tab strip shows the red request with its output.

### @LETTER@3. A conflict starts one merger

1. Push two branches that change the same line. Run `atrium merge` for both, one after the other.

**Expected:** the first lands. The second is a conflict, one slim merger card starts for it, and when it is done the
second request is landed with the merger's sha and both branches are deleted.

### @LETTER@4. A card cannot push claude/main

1. From a card, `git push hub HEAD:claude/main`.
2. Set the merger role: `atrium_flow role_set` with role `merger`.

**Expected:** the push is refused with "only the operator and the merge queue move claude/main". The role is refused
with "the merge queue is the merger now: atrium merge".

### @LETTER@5. Land now

1. With at least one landed request, open the Terminals tab and press **Land now** (or run `atrium merge land`).
   Answer no to the confirmation once, then yes.
2. Land a second branch on `claude/main` and press **Land now** again.

**Expected:** answering no posts nothing. After yes the hub's `main` is one new commit whose parent is the old `main`,
its tree `claude/main`'s, its message one line, authored as the operator and unsigned, and `refs/atrium/lands/<n>`
is that commit. `claude/main` did not move, and no `land/<n>` branch exists. Neither the board nor the CLI shows a push
command. The second land is a commit on top of the first.
