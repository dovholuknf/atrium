## Test plan

## @LETTER@. Closing a card deletes its hub branches

Setup: a room with `git.push` set to hub, and a card that has pushed `claude/x` with `atrium_git_push`.

### @LETTER@1. A merged branch goes with the close

1. Merge `claude/x` into `claude/main` and close the card.

**Expected:** the close lists the hub branch as freed and `claude/x` is gone from the hub, with no question asked.

### @LETTER@2. An unmerged branch is asked about

1. Push `claude/y` with a commit `claude/main` lacks, and close the card.

**Expected:** the close asks keep or delete for `claude/y`. Keep leaves it on the hub and the row live. Delete removes it.

### @LETTER@3. No push deletes what is not yours

1. From a clone, `git push hub --delete main`, then `--delete claude/main`, then `--delete` a tag, then another card's
   branch.

**Expected:** each is refused with `remote: atrium: ...`. Deleting your own `claude/*` branch the same way works.

### @LETTER@4. An unreachable hub does not block the close

1. Stop the hub and close a card that has a hub branch.

**Expected:** the close finishes, reports that the hub branch was not deleted, and leaves its row live.
