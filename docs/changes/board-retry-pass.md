## Test plan

## @LETTER@. A board unit that passes on retry is a marked pass

### @LETTER@1. The sharded runner exits 0 and marks the unit

1. Run `node scripts/test-board-sharded-retry.js`. It drives `scripts/test-board-sharded.js` against a stand-in harness
   with a unit that fails once, one that always fails, and one that always passes.

**Expected:** it prints `ok`. A unit that fails first and passes on retry gives exit 0 with a table row reading
`PASSED ON RETRY (slow or stalled, first try failed)`, a `PASSED ON RETRY, NOT A CLEAN PASS: <unit>: ...` line when the
retry lands, a block with the first-try error and both timings, and a totals line counting it apart from clean passes.
A unit that fails both tries, or any failure with `--no-retry`, still exits 1.

### @LETTER@2. The run summary, rollup and ledger list it

1. Run `node scripts/test-ci-rollup.js`.
2. Put a `retried.tsv` (step, unit, first error, timings) in a job folder of a scratch artifact tree, then run
   `LEDGER_DIR=<dir> CI_ARTIFACTS=<tree> node scripts/ci-flake-ledger.js ingest <run>` and `report`.

**Expected:** the rollup prints `ok`, and a retried unit is a warning annotation and a table section, never an error.
The ledger line says `N passed on retry` and the report has a `passed on retry` column. `ci.sh` writes the same rows
under `## PASSED ON RETRY, NOT CLEAN PASSES` in `summary.md`, and its title line says `PASSED, BUT n BOARD UNIT(S)
PASSED ONLY ON RETRY`.
