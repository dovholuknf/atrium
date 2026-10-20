Docs: none, run_log is added to docs/review/pulls-api.md and nothing else a reader names changed

## Test plan

## @LETTER@. Adding a PR says what it is doing

### @LETTER@1. Live steps on the pulls row

1. Add a PR from the Pulls tab (or open a PR link with ctrl-k).
2. Watch its row while it fetches and primes.

**Expected:** the row lists each sub-step as it happens (fetch, worktree, agent launch) with an elapsed timer on the
running one and `(X.Xs)` on each finished one. Nothing sits on a bare spinner.

### @LETTER@2. One forge ask

1. Add a PR on a hub-backed forge and read `run.log` of the PR.

**Expected:** the fetch step shows one view and diff round trip, not a view, a second view and a diff.
