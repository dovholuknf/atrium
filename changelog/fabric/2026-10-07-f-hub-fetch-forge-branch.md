# The hub fetches a forge branch a card asks for

- `atrium_git_url` (and `GET /_hub/git/url`) with a branch that neither the hub's store nor an attached room has now asks the repository's forge. If the forge has the branch, the hub fetches it into `refs/forge/<branch>` and answers a `hub` source with `forge: true`. Design: docs/fabric/hub-forge-design.md 3.5.
- `refs/forge/` is a namespace of its own. `main` and a room's pushed branches are never written over, and a room's pushed branch of the same name always wins.
- The store's fetch route also advertises each forge branch under `refs/heads/<branch>` when no pushed branch has that name. So `git fetch hub <branch>` just works, and `git fetch hub forge/<branch>` always gets the forge's copy.
- The first fetch of a branch comes from the lookup. After that, each ask refreshes it, and so does each fetch through the route (at most once every 10 s per repository, bounded to 20 s). A branch the forge deleted is dropped.
- A repository the hub does not hold yet is made, as `atrium hub git init` makes it, when the forge has the branch asked for. A typo makes nothing.
- A public repository needs no credential. A private one uses the PR head fetch's login, the forge CLI's credential helper (`Hub.ForgeHelper`, wired from the hub's forge entries). With no login the answer is the new `no credential` state, "the hub has no credential for <repo>", and not `not found`.
- Bounds: one fetch per branch at a time, the seed's 2-minute timeout, and at most 100 forge branches per repository. Branch names are checked with `check-ref-format` and never taken as an option. Nothing is ever pushed to a forge.
