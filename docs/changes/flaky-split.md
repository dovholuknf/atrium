## Test plan

## @LETTER@. The flaky suite runs as three jobs per platform

### @LETTER@1. The groups are listed and the halves cover the suite once

1. Run `bash scripts/ci.sh --list-groups` and `node scripts/flaky-suite.js check`.
2. Source `board_units` from `scripts/ci.sh` and compare `flaky-1` with `flaky-2`, and `flaky-scripts-1` with `flaky-scripts-2`.

**Expected:** `flaky-go`, `flaky-board`, `flaky-board-1` and `flaky-board-2` are listed. The two halves together hold
every flaky board unit and every flaky check script, each exactly once, at about 143 s each by the weight file.

### @LETTER@2. Each job installs only what it needs

1. Read a CI run's `flaky-go` job: it has no setup-node step. Read a `flaky-board-1` job: it has no setup-go step.
2. Open each job's artifact `ci-<os>-latest-<group>-attempt-1`.

**Expected:** `flaky-go` holds `go-flaky/`, the board jobs hold `flaky-board.log` and `flaky-env.txt`, and every job is
green. A red flaky job skips every regular job. A board unit that fails and passes alone is marked PASSED ON RETRY and a
Go test gets no retry. Two shards of about 11 units is the agreed trade: each half runs its units serially.
