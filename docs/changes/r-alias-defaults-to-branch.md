## Test plan

## @LETTER@. A new card's alias defaults to its branch

### @LETTER@1. Launch on a branch

1. Launch a worker with no alias and a title without a prefix, in a worktree on branch `claude/u-foo`.

**Expected:** the card's alias is `@u-foo`, and `atrium_say @u-foo` reaches it.

### @LETTER@2. A second card on the same branch name

1. Launch another worker on branch `other/u-foo` while the first is live.

**Expected:** its alias is `@u-foo-2`. A third gets `@u-foo-3`.

### @LETTER@3. The title and a typed alias win

1. Launch a worker titled `sa89: typing gate` on branch `claude/u-bar`.
2. Set an alias by hand on a card, then join that session again.

**Expected:** the first card is `@sa89`, not `@u-bar`. The hand-set alias is unchanged.

### @LETTER@4. No default for main, master or no branch

1. Launch workers on `main`, on `master`, on a detached HEAD, and in a folder that is not a repo.

**Expected:** none gets an alias, as before.

### @LETTER@5. Join and adopt

1. In a worktree on branch `claude/u-join`, run `atrium join` from a session that is on the board with no alias.
2. Start an unknown `claude` session in a worktree on another branch and let it ask for a permission.

**Expected:** the first card becomes `@u-join`, and the second card takes its own branch as its alias.

### @LETTER@6. Existing cards stay as they are

1. Restart the daemon with cards on the board that have no alias.

**Expected:** none of them gains an alias from its branch.
