## Test plan

## @LETTER@. The flaky jobs follow the flaky suite

### @LETTER@1. Empty list

1. Run `bash scripts/ci-select.sh` with `scripts/flaky-suite.json` holding no entries.

**Expected:** `flaky_ubuntu`, `flaky_macos` and `flaky_windows` are `[]`, `skipped=3`, the check outputs are unchanged.

### @LETTER@2. A failure turns the jobs on

1. Run `node scripts/flaky-sync.js <run id>` for a run with a failing test while the list is empty.
2. Run `bash scripts/ci-select.sh` again.

**Expected:** the test is an entry and the flaky outputs list `flaky-go` and `flaky-board` again.
