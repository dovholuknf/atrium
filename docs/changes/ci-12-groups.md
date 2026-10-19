Docs: none, CI only

## Test plan

## @LETTER@. CI runs 12 jobs

### @LETTER@1. The merged groups

1. Run `bash scripts/ci.sh --list-groups`.

**Expected:** `go-all` lists go-build, go-test, time-guard, the go-checks steps and board-visual. `go-checks-visual` is
the go-checks steps plus board-visual. Each ends with `clean-tree`.

### @LETTER@2. board-visual alone is not a failure

1. In a copy of `scripts/ci.sh`, make `board_visual_run` print `FAIL x` and return 1.
2. Run the copy with `--only board-visual,gofmt,clean-tree`.

**Expected:** the log says `FAILED: board (pixel and timing units)`, the summary reads `PASSED (board-visual failed,
which does not fail the job)`, `failing.tsv` has a `board-visual` row, and the exit status is 0.

### @LETTER@3. Any other failure still fails the job

1. In the same copy, make gofmt report a file as well.

**Expected:** exit status 1 and the summary reads `FAILED`, with the `board-visual` row still in `failing.tsv`.

### @LETTER@4. The workflow

1. Push to a branch and open the run.

**Expected:** 12 jobs: ubuntu 3 (`go-all`, `board-1`, `board-2`), macos 4 (`go-test`, `go-checks-visual`, `board-1`,
`board-2`), windows 5 (`go-test-1`, `go-test-2`, `go-checks-visual`, `board-1`, `board-2`).
