A pasted pull request link opens its card in one call, `POST /v1/open {url, why?}`, on the hub and on a room alike.
The room does the whole chain the launch dialog used to run in the browser: recognise the link, make or find the
worktree, make the review row, launch the card in the worktree with the recogniser's title, prompt and tags (plus
`link:<key>`), and set it as the row's walker. A step that fails undoes what the earlier steps made, newest first: the
card's launch failing removes the worktree and the branch it made and archives the row, so a retry starts clean. A
second paste of a link whose walker card is live answers that card (`created: false`) and starts nothing. No provider
row is needed: a host with none uses the scm clone and `<scm folder>/worktrees/<host>/<org>/<repo>/<branch>`. On the
hub the call goes to the room that already holds the pull request, else the least busy one, or to the room the caller
named, and the answer comes back with `room` and the card and row ids tagged. The launch dialog (and so ctrl-alt-r and
a paste on the board) and the pulls tab's paste field call it, and attach the card. The dialog's edited title, prompt,
runner, model and effort go with it. Only pull requests so far: an issue or a support link answers `not_a_pr`, and
the launch dialog still handles it the old way. Design: `docs/rnd/card-lifecycle-design.md` section 3, phase 2.

Test plan:
- On the hub board, with no room picked, paste `https://github.com/openziti/ziti-console/pull/967` in the pulls tab.
  A worktree appears on the least busy room, the review row starts, and the walker card opens attached.
- Paste the same link again, in the pulls tab or with ctrl-alt-r. The same card is attached and no second one starts.
- Ctrl-alt-r, paste a pull request, shift-enter, change the prompt and press launch. The card starts with that prompt.
- On a room with no provider for github.com, open a pull request. The worktree lands under the scm folder's
  `worktrees/github.com/...`.
- Disable every runner and open a pull request. The dialog says the card did not start, and the worktree and the row
  it made are gone.
