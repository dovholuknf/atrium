# r-new-hub-remote: the room's stable `hub` remote, and cards pushing to it only

Status: HELD (the pause). Filed by @rnd 2026-10-02 from `docs/fabric/hub-forge-design.md` revision 2, stage 1 (sections
5.1 and 5.3). Owner @runtime. Size about 1.5 days. Needs @fabric's `f-new-hub-receive`.

- A stable forwarder on the agent listener: `/git/hub/<host>/<owner>/<repo>.git` (and, in stage 3,
  `/git/room/<room>/...`), forwarded to the hub over the link's `git` kind.
  - It requires the card's atrium token.
  - It sends the card id with the request.
  - A request with no token is refused.
- At launch, the card's environment carries the token for git as `http.http://127.0.0.1:<port>/git/.extraHeader`,
  scoped to the forwarder and never bare, so no other remote ever receives it. It is never written into
  `.git/config`. A card that runs outside code (PR tests, `prove`) gets no token and `git.push=none`.
- `atrium_git_push` and the hook check that `git remote get-url --push <remote>` (the URL after every
  `insteadOf`/`pushInsteadOf` rewrite, not the raw config) is the forwarder's. A test sets a global
  `pushInsteadOf` that rewrites the forwarder's URL elsewhere, and the push must be refused. Where a clone's own
  `hub` points elsewhere, atrium's remote is added as `atrium-hub`.
- The room setting `git.push`: `none` or `hub` (default `hub`). With `none`, the forwarder refuses receive-pack.
- `atrium_git_push {branch}`: the same push without a shell, under the same rules.
- `hub` is added to the clones the room already syncs (git-sync stage 1). An existing `hub` remote that points
  elsewhere is left alone and reported.

Acceptance: design section 7, row `r-new-hub-remote`. The dotfiles hook change in the same table, which allows
exactly `git push hub <branch>`, is clint's or the orchestrator's.
