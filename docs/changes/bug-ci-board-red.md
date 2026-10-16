# bug-ci-board-red

The summary CI writes (`build.claude/ci/summary.md`, copied to the run page) gains two things, both produced by
`scripts/ci.sh`:

- the Steps table has `tests run` and `tests failed` columns. Steps that run no tests show `-`. go-test and go-race count
  Go tests and subtests (`scripts/ci-report.go` writes `go-test.counts`), board counts units, ps-tests counts its `ok` and
  `FAIL` check lines.
- a Failing tests section lists each failed test, unit or check with its step and first error line.

The board step's report is kept as `build.claude/ci/board.log` and parsed from there. go-race now runs `go test -json`
through `ci-report.go` into `build.claude/ci/race/`, the same as go-test.

The board units that failed run 38890140075 were fixed in bug-ci-board-red-10-units.

## Test plan

## @LETTER@. CI summary counts and failing tests

### @LETTER@1. A forced failure shows up

1. Make a Go test fail on a branch and run `bash scripts/ci.sh --only go-test`.
2. summary.md has a `tests run` and `tests failed` column on the go-test row and a Failing tests table naming the test
   with its first error line.

Proof: docs/changes/img/bug-ci-board-red/summary-forced-failure.md is summary.md from a local go-test run with one forced failing test (the other failures are this machine's known environment ones).
