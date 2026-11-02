## Test plan

## @LETTER@. Three test seams moved off package variables

### @LETTER@1. Behaviour is unchanged

1. Open a link to a GitHub security advisory fork's branch, then a pull request of it.
2. Start a session inside a card's own runner, and let a task's runner outlive its exit wait.

**Expected:** the fork is fetched over https from `<host>/<org>/<repo>-ghsa-<id>`, a nested session cannot take the card,
and a runner alive after 60 seconds fails the capture with the folder kept, as before.
