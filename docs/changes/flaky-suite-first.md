## Test plan

## @LETTER@. The flaky suite runs first and alone

### @LETTER@1. The manifest names real tests

1. Run `node scripts/flaky-suite.js check`.
2. Misspell one test name in `scripts/flaky-suite.json` and run it again, then undo the edit.

**Expected:** the first run prints `ok` and the entry count. The second exits 1 and names the missing test.

### @LETTER@2. Each test runs in one place

1. Run `bash scripts/ci.sh --list-groups` and read the `flaky` line.
2. Run `bash scripts/ci.sh --only go-build` and check `flaky suite names` passes.
3. Compare the board halves with the manifest: `board_units gating-1`, `gating-2`, `visual` and the manifest's board
   units together are every unit the harness lists, and no manifest unit is in the first three.

**Expected:** the group is `flaky,clean-tree`. No test appears in two of go-test, board, board-visual and flaky.

### @LETTER@3. The flaky job comes first on each platform

1. Push a branch and open the run.
2. Check that `check (<os>-latest, flaky)` starts before any other `check` job of that OS.
3. Break one manifest test on purpose, push again.

**Expected:** the flaky job of that OS is red, the other jobs of that OS still run and report, and `notify` fires.

### @LETTER@4. test-touched runs the flaky tests first

1. Touch a file in `internal/ptyhost` and run `bash scripts/test-touched.sh`.

**Expected:** a `flaky suite: go test ./internal/ptyhost` step runs before `go test (...)`, and the later run skips
that test.
