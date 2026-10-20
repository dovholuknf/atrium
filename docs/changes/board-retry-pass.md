## Test plan

## @LETTER@. A flaky-listed board unit that passes on retry is a marked pass

### @LETTER@1. The sharded runner tolerates a retried pass for a listed unit only

1. Run `node scripts/test-board-sharded-retry.js`. It drives `scripts/test-board-sharded.js` against a stand-in harness
   and a stand-in flaky list (`BOARD_FLAKY_FILE`) with a listed unit that fails once, an unlisted one that fails once,
   one that always fails and a listed one that always fails.

**Expected:** it prints `ok`. The listed unit that fails first and passes on retry gives exit 0 with a table row
reading `PASSED ON RETRY (slow or stalled, first try failed)`, a `PASSED ON RETRY, NOT A CLEAN PASS: <unit>: ...` line
when the retry lands, a block with the first-try error and both timings, and a totals line counting it apart from clean
passes. The unlisted unit that fails first and passes on retry exits 1 with `LOAD (passed on retry)`. A unit that fails
both tries, listed or not, or any failure with `--no-retry`, exits 1.

### @LETTER@2. The flaky job retries its board units

1. Read the flaky step of `scripts/ci.sh`.

**Expected:** `test-board-sharded.js` runs there without `--no-retry`, so a listed board unit that fails and then
passes leaves the job green with the marked block. A listed go test still gets no retry.

### @LETTER@3. The run summary, rollup and ledger list it

1. Run `node scripts/test-ci-rollup.js`.
2. Put a `retried.tsv` (step, unit, first error, timings) in a job folder of a scratch artifact tree, then run
   `LEDGER_DIR=<dir> CI_ARTIFACTS=<tree> node scripts/ci-flake-ledger.js ingest <run>` and `report`.

**Expected:** the rollup prints `ok`, a listed retried unit is a warning annotation and a table section, and an
unlisted one is still an error row saying `passed on retry, not on the flaky list, fails the job`. The ledger line says
`N passed on retry` and the report has a `passed on retry` column. `ci.sh` writes the listed rows under
`## PASSED ON RETRY, NOT CLEAN PASSES` in `summary.md` and counts the unlisted ones as failed.
