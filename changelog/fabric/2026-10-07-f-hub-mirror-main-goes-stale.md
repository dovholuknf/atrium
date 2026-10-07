# The hub's main follows its forge

- A fetch through the hub's store and an `atrium_git_url` lookup now bring `main` up to the forge's default branch first (at most once every 10 s per repository, bounded to 20 s). A card no longer gets a `main` stuck at the seed. Item f-hub-mirror-main-goes-stale.
- The forge's default branch is kept in `refs/forge/<default>`. `main` fast-forwards to it, and follows a forge's rewrite only while no operator has pushed `main`. An operator's `main` the forge does not have stays, and `git fetch hub forge/main` gets the forge's.
- A forge that cannot be reached leaves `main` as it is, and the fetch goes on. An adopted `git_repos` mirror is never touched.
