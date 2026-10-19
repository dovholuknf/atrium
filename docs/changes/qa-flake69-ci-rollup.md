Docs: none, a CI summary page has no reference page

# qa-flake69-ci-rollup

`scripts/ci-rollup.js` reads the current run's `ci-<os>-latest-<group>-attempt-<n>` artifacts and the last 10 completed
push runs of `ci` on main, and prints a markdown rollup. `--local DIR` reads a folder instead of the API. The `rollup`
job in `ci.yml` appends it to `$GITHUB_STEP_SUMMARY`. `scripts/ci-failures.js` holds the parsing the flake ledger also
uses. `scripts/test-ci-rollup.js` is its test.

## Test plan

## @LETTER@. The ci run page rolls up every failing unit

### @LETTER@1. A failing run

1. Open the run page of a ci run with a failed job, under the job graph.

**Expected:** a section "ci run N: K failing units" with one row per unit and job: os, job, unit, first error, retry
and history. A unit that passed alone says "passed on retry, fails the job". A unit never seen in the last 10 runs says
NEW, the others say "failed in N of the last 10".

### @LETTER@2. A green run and the failure modes

1. Open a green ci run.
2. Look at the `rollup` job of any run.

**Expected:** a green run says "green" in one line. The `rollup` job is green even when it could not read an artifact,
and `notify` still ran.
