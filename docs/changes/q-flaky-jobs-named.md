## Test plan

## @LETTER@. The flaky jobs have static names

### @LETTER@1. The selection and the readers

1. Run `node scripts/test-ci-select.js`, `node scripts/test-ci-rollup.js` and `node scripts/test-flaky-sync.js`.
2. Run `bash scripts/ci-select.sh --os macos --suite ui`.
3. Run `yq '.jobs | keys | length' .github/workflows/ci.yml`.

**Expected:** the tests print ok, the script prints `flaky_macos_playwright=true`, the other five flaky flags `false`,
`check_macos` with board-1 and board-2 and `skipped=7`, and the job count is 12.

### @LETTER@2. The run

1. Push a branch and open the run's checks list.

**Expected:** six jobs named flaky-linux-go through flaky-windows-playwright, no raw `${{ matrix.group }}`, a skipped one
shows its plain name. flaky-linux-playwright always shows skipped, as ubuntu never ran a flaky board group. The artifacts
keep their `ci-<os>-latest-flaky-go` and `flaky-board` names, and the rollup and flaky-sync read the new job names.
