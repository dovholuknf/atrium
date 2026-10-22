# A job that failed on an unlisted retry pass is recorded

Run 39795317388 failed `check (windows-latest, board-1)` with `FAILED IN THE SHARDS, PASSED ALONE, AND NOT ON THE FLAKY
LIST (1)` for `mResume`, but its `failing-tests` artifact said `"jobs": []`, so `flaky-sync.js` recorded `(no failed job)`.

- `ci-rollup.js` `failingDoc` adds a job for each os and job that has failing units in the downloaded artifacts and is
  not in the API's failed-job list.
- `flaky-sync.js` treats a downloaded artifact with no jobs as absent and reads the run's job list and logs instead.
- Held by `scripts/test-ci-rollup.js` and `scripts/test-flaky-sync.js`.

## Test plan

## @LETTER@. Unlisted retry pass is recorded

1. Run `node scripts/test-ci-rollup.js` and `node scripts/test-flaky-sync.js`.

**Expected:** both print `ok`.
