A forge's short label and its host name are one host. `github.com`, `bitbucket.org` and `gitlab.com` are drawn as
`github`, `bitbucket` and `gitlab`, the label the hub already uses. The rule is `forgeHostLabel` in `js/cardrules.js`
and `store.ForgeLabel` in Go. It is applied where the board builds a card's label (`terminalLabel`), so the terminals
tree, the filters and the repo colours agree. URLs to the forge are untouched.

## Test plan

## @LETTER@. One forge, one group

### @LETTER@1. github and github.com group together

1. Have a card whose worktree is `.../github/openziti/ziti-sdk-c/a` and one at `.../github.com/openziti/ziti-sdk-c/b`.
2. Open the Terminals list grouped by project.

**Expected:** one `github` group holds both. No `github.com` group exists.

### @LETTER@2. A self-hosted forge keeps its name

1. Have a card whose worktree is `.../git.example.com/o/r/a`.

**Expected:** it sits under `git.example.com`, unchanged.
