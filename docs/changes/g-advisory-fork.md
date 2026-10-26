## Test plan

## @LETTER@. Advisory fork links

### @LETTER@1. Open a fork branch and a fork pull request

1. Paste a link to a branch on an advisory's temporary fork, like `https://github.com/<org>/<repo>-ghsa-<id>/tree/<b>`.
2. Paste a link to a pull request on the same fork, like `https://github.com/<org>/<repo>-ghsa-<id>/pull/1`.

**Expected:** each opens a card on a worktree of `<org>/<repo>`. The first is on branch `<b>` tracking
`ghsa-<id>/<b>`, the second on `advisory-<id>-pr1` at the pull request's head. `git remote` in the clone lists
`ghsa-<id>`.

### @LETTER@2. Close both

1. Close the first card, then run `git remote` in the clone.
2. Close the second card, then run `git remote` again.

**Expected:** the remote is still there after the first close and gone after the second, with no `ghsa-<id>` refs left.
