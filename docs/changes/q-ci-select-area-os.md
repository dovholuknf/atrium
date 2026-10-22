## Test plan

## @LETTER@. A dispatch runs one os and one suite

### @LETTER@1. The selection

1. Run `node scripts/test-ci-select.js` and `node scripts/test-ci-notify.js`.
2. Run `bash scripts/ci-select.sh --os windows --suite ui`.

**Expected:** both tests print ok, and the script prints `flaky_windows=["flaky-board"]`, `check_windows` with board-1 and
board-2, an empty `[]` for the other four jobs and `skipped=4`.

### @LETTER@2. The dispatch

1. Run `gh workflow run ci.yml --ref main -f os=windows -f suite=ui`.

**Expected:** only the windows flaky-board and board-1 and board-2 jobs run, the other jobs show skipped, and the run is
green, not red. A plain push still runs all 17 groups.
