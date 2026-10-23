## Test plan

## @LETTER@. CI jobs are balanced and the board jobs install no Go

### @LETTER@1. The groups

1. Run `bash scripts/ci.sh --list-groups`.

**Expected:** `board-1-visual`, `build-board-1` and `race-board-2` are listed, and `go-all` has neither `go-race` nor
`board-visual`.

### @LETTER@2. A board group needs no Go

1. Run `bash scripts/ci.sh --group board-1 --only toolchain` with Go off the PATH.

**Expected:** the toolchain step passes and names no Go problem.

### @LETTER@3. The run

1. Read the job times of a push run.

**Expected:** the slowest regular job is near 400 s and the job count is 9 regular plus flaky, rollup and notify.
