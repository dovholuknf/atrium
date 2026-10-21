## Test plan

## @LETTER@. Changes for a turn with hundreds of paths

### @LETTER@1. A turn that edited 600 files

1. On a card whose transcript has one reply that edited 600 files with long paths, all changed in the worktree, open
   `changes?turn=<reply time>`.

**Expected:** 200, `total` 600, the list cut at the files maximum, with the cut counting the rest. Paths are batched
about 24000 characters to a git command, so there are half as many git runs as before.
