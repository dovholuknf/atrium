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
2. Press it again before the push is run.

**Expected:** the hub has `land/<n>` with one commit on `main`, its tree `claude/main`'s, its message one line, signed
when the operator's git config signs. `claude/main` is that commit, and the old tip is under `refs/atrium/lands/<n>`.
The board shows the pending land with a button that copies the push. The second press is refused until the push is
run and the hub's `main` has the commit.
