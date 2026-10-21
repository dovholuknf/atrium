## Test plan

## @LETTER@. The flaky groups run after the checks

### @LETTER@1. The needs graph

1. Run `yq '.jobs | keys' .github/workflows/ci.yml`.
2. Run `yq '.jobs.[] | .needs' .github/workflows/ci.yml` and read each job's list.

**Expected:** 8 jobs. The three `check-*` jobs have no `needs`. Each `flaky-*` job needs all three `check-*` jobs.
`rollup` needs all six and `notify` needs `rollup`.

### @LETTER@2. A failing check

1. Push a branch with one check test broken on purpose.
2. Open the run.

**Expected:** the checks start at once, the failing check is red, the `flaky-*` jobs are skipped, `rollup` still runs,
and `notify` reports failure.
