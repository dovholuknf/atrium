## Test plan

## @LETTER@. A LOAD pass passes the run

### @LETTER@1. Retry pin

1. Run `node scripts/test-board-sharded-retry.js`.

**Expected:** it exits 0. An unlisted unit that fails first and passes on retry gives exit 0 with the LOAD row, its block
and the totals line naming it. A unit that fails both tries still exits 1.

### @LETTER@2. CI summary

1. Feed `board_report` in `scripts/ci.sh` a log with one LOAD row.

**Expected:** it counts 0 failed and hands the unit to the PASSED ON RETRY section of `summary.md`, marked as LOAD.
