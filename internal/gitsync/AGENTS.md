# internal/gitsync

Git between the hub and its rooms. Git plumbing only: it knows nothing about the link, which carries its bytes.
`docs/fabric/git-sync-design.md` is the mirror and sync, and `docs/fabric/hub-forge-design.md` is the hub's own
store, the forwarders, the pass-through and the lookup.

## Two halves

Code moves by FETCH only in the **`git_repos` mirror and sync** (`hub.go`, `sync.go`, `room.go`): the hub mirrors
each configured repository, tells rooms to fetch from it, and collects each room's `claude/*` branches back by
fetching them.

The **hub's own store** (`store.go`, `receive.go`) is bare repositories at `<git.store>/<host>/<owner>/<repo>.git`,
served over smart HTTP at `/git/hub/...`, which DO take pushes from cards through the room's forwarder. Read the
header of the file you are in, not only the package comment.

## Invariants

- **The integration branch is the only branch a room syncs from the hub.** `IntegrationBranch` is not a parameter,
  and no setting, tool, route or verb takes another. A room's `hub-main` that once followed a department branch put
  workers on unmerged work.
- **No server policy is ever written into a repository.** All of it rides the CGI environment (`GIT_CONFIG_COUNT`),
  which is why `MinGit` exists. The pre-receive hook lives in a directory the hub writes and `core.hooksPath` points
  at, never in the repository.
- **Every git command goes through a `Runner`.** Bounded by `CommandBound` (ten minutes), killed at the bound, and
  cancelled together on `Stop`, so shutdown leaves no git child holding a ref lock. On Windows each child goes in a
  job object, because `git-remote-http` is not ended when its parent is killed.
- **A push is checked BEFORE git sees it.** `receive.go` spools the body under a cap, parses the command list, and
  applies every rule that needs no objects: who is pushing, ref names, main, tags, deletes, a stale old sha, case
  collisions, branch ownership. Only fast-forward needs the pack, and that is the hub's pre-receive hook. The
  repository's lock is held from the rule check through receive-pack and the log append, so two racing pushes cannot
  both pass the ownership check.
- **The caller is set by the listener a request came in on**, never by the request (`identity.go`). Card headers are
  read only from a Room caller and dropped unread on every other reach.
- **The room's forwarder DELETES any card header the client sent and Sets its own.** A client must not choose its card.
  The card token authorizes the forwarder route and nothing else, and is never forwarded to the hub. Tokens live in
  the store, so a runner kept through a room restart keeps a good one.
- **The token header is scoped to the forwarder's URL** with `http.<url>.extraHeader`. Never a bare
  `http.extraHeader`, which goes to every remote.
- **What a room serves is its own configuration** (`ServedHide`). Protocol v0 only, so a want that is not an advertised
  tip is refused by the room's git. `claude/main`, `hub-main` and the clone's own default branches are never served,
  since that is where the operator's unpushed work may be.
- **The hub stores nothing for a pass-through fetch.** It reads the want list into memory, refuses `shallow`, `deepen`
  and `filter`, rate limits per reader and caps concurrent fetches per room, and streams the answer.
- **A branch name a caller gives the lookup is only ever compared** to names the hub and rooms listed. It is never given
  to git, made into a path or made into a ref.
- **A room never fetches from a forge.** The hub fetches PR heads into `refs/atrium/pr/<N>` and forge branches into
  `refs/forge/<branch>`, never `refs/heads`, and nothing is ever pushed to a forge.
- **Main follows its forge only when nobody but the seed wrote it.** A card can never push main, so pushed work on main
  is the operator's, and the push log decides. With an operator push, or no push log, main stays as pushed.
- **The epoch reset is the one move that is not a fast-forward**, so it is done by the hub after checks, not by a push.
  The old tip is written to `refs/atrium/epochs/<n>` first, so no commit becomes unreachable.

## Landmines

- **`CleanLocks` removes stale `*.lock` files ONLY under refs the caller owns.** A git killed mid-fetch leaves locks
  and every later git fails with "File exists". Everything else in a clone is the operator's, `packed-refs` included,
  and a stale lock there is reported, not removed.
- **Refusals must look like receive-pack's** (`remote: atrium: <sentence>`, `! [remote rejected]`), so a person's
  git shows them.
- **Ref names pass a strict ASCII allowlist before git's own check** (`refname.go`), because a branch lands as a file
  name on an NTFS disk and goes into logs and the board.
- **A Cygwin or MSYS git is refused on Windows.** It writes `/cygdrive` gitfiles Git for Windows cannot resolve.
- **`git http-backend` runs through `cgi.go`, not `net/http/cgi`**, so the child goes through `nowindow.Hide` and the
  hub does not open a console window per fetch.
- **The lifecycle reads every item's changelog entry from one `git log` per ref tip** (`landindex.go`), and every
  other answer about shas (patch ids, ancestry, paths) once, shared by readers that ask together. Never a
  git call per item, and none while no tip has moved.
- **The board's repo list and `/_hub/git/pushed` are read from snapshots** (`viewcache.go`, `pushedcache.go`). Do not
  compute them on the request. A process spawn is 50 to 100ms on Windows.
- **A stash pushed by a close expires** after `git.stash_retention_days` (30 by default, 0 for never). One the push log
  has no record of is kept.
- **Most tests carry `//go:build integration && git`** and run only under `scripts/test.sh full`.

## The files

| | |
| --- | --- |
| `run.go` | the package comment, `Runner`, `CommandBound` and output caps |
| `hub.go` / `sync.go` / `room.go` | the `git_repos` mirror, telling rooms to sync, collecting `claude/*` back |
| `repos.go` | the `git_repos` setting. The branch is the integration branch and nothing else |
| `store.go` / `store_name.go` | the hub's own bare repositories and their names |
| `receive.go` / `receive_rules.go` | pushes to the store: the rules checked before git, the hook, the lock |
| `backend.go` / `cgi.go` | `git http-backend` with everything but what is needed switched off |
| `forward.go` | a loopback listener that lives for one git command |
| `hubremote.go` / `hubpush.go` / `cardtoken.go` | the room's stable forwarder to the store, and the card token |
| `identity.go` | who is on the other end of a request to the store |
| `clonerem.go` | the `hub` remote, and removing the retired `remote.origin.pushurl` guard |
| `served.go` | what a room offers a reader the hub passes through |
| `pass.go` | a fetch passed through the hub to the room that has the work, with its limits and deadlines |
| `lookup.go` / `lookup_commit.go` | `atrium_git_url`: which source has a branch, which branch has a commit |
| `pushed.go` / `pushedcache.go` / `pushlog.go` | what the hub knows of a branch, and the push log |
| `storeforge.go` / `storepr.go` | forge branches and PR heads fetched into the store |
| `flowepoch.go` / `flowfacts.go` | the merge flow's reset of claude/main, and whether a commit is in a branch |
| `merge.go` | the merge queue: the candidate by `merge-tree`, the locked move of claude/main, deleting a landed branch |
| `mergerun.go` | the room testing a candidate in its own merge repository, and pruning a landed local branch |
| `stashexpiry.go` | expiring a close's stash branches |
| `scm.go` | clones in the operator's scm folder, and which hosts the credential helper may be asked about |
| `locks.go` | removing stale lock files, only where the caller owns the ref |
| `shared.go` | whether two directories share one repository |
| `gitver.go` | the oldest usable git, and refusing Cygwin and MSYS |
| `pktline.go` / `refname.go` | just enough pkt-line, and the ref name allowlist |
| `tree_*.go` | ending a git child and everything it started |
| `viewcache.go` | the board's repository list, from a snapshot |
| `landfacts.go` / `landindex.go` | an item's landing and pushing, and every changelog entry read once per ref tip |
