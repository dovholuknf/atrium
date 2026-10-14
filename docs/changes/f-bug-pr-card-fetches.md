## Test plan

## @LETTER@. A pull request card reads everything locally

### @LETTER@1. Open a pull request

1. Open a github pull request link on a room.
2. The card's first prompt names the base branch, a merge base sha, the title and says not to fetch, pull or run gh.
3. `.atrium/PR.md` in the worktree holds the description and the discussion, and `git status` does not list it.
4. `git diff <merge base> HEAD` in the worktree shows the pull request's change with no fetch.
