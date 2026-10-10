# f-hub-gets-run-git-on-request

- internal/gitsync/pushedcache.go: `Store.PushedSnapshot`, a per (repo, branch, head) snapshot rebuilt on a push, an
  init and the view timer. No git on a GET. Answers are forgotten after ten minutes unasked, at most 512 kept.
- internal/gitsync/viewcache.go, store.go: `RefreshView` rebuilds the pushed answers too, and the view loop starts on
  the first ask of either.
- internal/link/changerequest.go: git/pushed answers from it with `building`, `computed`, `age_seconds`. A change
  request's `pushed` is null until its first build is done.
- internal/link/deployready.go: GET deploy-ready answers from the last pass (`readyCached`) and starts a pass in the
  background when it is old, absent or `fresh=1`. The deploy POST still reads fresh and waits.
- internal/api/web/js/deployready.js: asks again while the hub says `building`.
- Tests: gitsync/pushedcache_test.go, and the deployready and changerequest integration tests in internal/link.

## Test plan

## @LETTER@. The hub's reads answer at once

### @LETTER@1. Pushed and deploy-ready from a snapshot

1. Open the board against a hub with a large repository.
2. Open a change request and the deploy pill, and note that both draw at once.
3. Check `GET /_hub/git/pushed` and `GET /_hub/deploy-ready` carry `building`, `computed` and `age_seconds`.
