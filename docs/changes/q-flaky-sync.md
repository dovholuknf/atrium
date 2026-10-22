## Test plan

## @LETTER@. The flaky suite follows the last 15 runs

### @LETTER@1. The sync

1. Run `node scripts/test-flaky-sync.js`.
2. Run `node scripts/flaky-sync.js 39710412601 --dry-run`.

**Expected:** the test prints `flaky-sync: ok`, and the dry run reports the window of 15 runs, nothing added, one entry
removed and nothing written.

### @LETTER@2. The artifact

1. Run `node scripts/test-ci-rollup.js` and `yq '.jobs | keys' .github/workflows/ci.yml`.
2. After the next main run, `gh run download <run id> -n failing-tests`.

**Expected:** the test prints `ok`, the workflow still lists 8 jobs, and the artifact holds `failing-tests.json` with
each failed job's id, name, failing tests and passed-on-retry tests.
