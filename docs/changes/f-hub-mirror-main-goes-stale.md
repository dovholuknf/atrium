## Test plan

## @LETTER@. The hub's main follows its forge

### @LETTER@1. A seeded main is current on a fetch

1. Pick a public repository the hub's store seeded some time ago, whose forge `main` has moved since (tlsuv).
2. In a clone with the hub as a remote, run `git fetch hub main`.

**Expected:** `FETCH_HEAD` is the forge's current `main` tip, as `git ls-remote https://github.com/<org>/<repo> main`
shows it. The hub's audit has a `git-store-forge-main` row for the repository.

### @LETTER@2. The lookup answers a current main

1. Push a commit to the forge's `main` (a test repository you own), and wait 10 seconds.
2. Call `atrium_git_url` for that repository and branch `main`.

**Expected:** the hub source's sha is the new forge tip.

### @LETTER@3. An operator's main is kept

1. As the operator, push to the hub's `main` a commit the forge does not have.
2. Push a different commit to the forge's `main`, wait 10 seconds, and run `git fetch hub main` then
   `git fetch hub forge/main`.

**Expected:** `main` is still the operator's commit. `forge/main` is the forge's tip. The audit has a
`git-store-forge-main-kept` row.

### @LETTER@4. A forge that cannot be reached

1. Block the hub's route to the forge (or point a test hub at an unreachable forge).
2. Run `git fetch hub main`.

**Expected:** the fetch succeeds within about 20 seconds and serves the `main` the hub already had.
