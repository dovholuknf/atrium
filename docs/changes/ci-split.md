Docs: none, scripts/AGENTS.md and docs/qa/ci-matrix.md are updated on the branch. Nothing a user names changed.

## What and why

A run of the CI workflow took 12 to 14 minutes, and that was the Windows board job (12.1 min) and the Windows and
macOS go-test jobs (10.1 and 10.2 min). Run 39040667475's artifacts say where the time went. The board step was 362 s
on ubuntu, 518 s on windows and 552 s on macos, and the windows board job also carried the PowerShell tests (80 s).
The go test step was 238, 504 and 299 s, and on windows internal/daemon was 481 of the 1479 package seconds, with
gitsync 232, link 204, store 180, hubstore 142 and api 133 behind it. A split by package can never beat daemon's 481 s.

A matrix job is a four CPU machine, so the way to halve the wall is more jobs, each over half the work:

- `go-test-1` and `go-test-2`. Daemon is cut by test name, `^Test[A-F]` against the rest (51% of its test seconds on
  windows, 44% on linux, 42% on macos). Part 1 is that half with gitsync, api, cli, ptyhost, deployready and roomspec.
  Part 2 is the other half of daemon with every other package from `go list`, so a new package is never left out. Each
  part is two `go test` invocations side by side, daemon's and the others' at `-p 3`, the four binaries a whole run
  has. Their streams are merged into the one `go-test.json` that the reporter, the summary and the time guard read.
- `board-1` and `board-2`. `board_units gating-1|2` halves the gating units by `scripts/board-suite-weights.json`,
  pin groups whole, longest first onto the lighter half, in suite order. Deterministic, and a partition: the two jobs
  never both run a unit and never both skip one.
- `go-checks` takes the skins, contrast, opencode, release and parse checks, once, on the linux job, and the
  PowerShell parse and tests on windows. The board jobs are the board and nothing else.

`go-test` and `board` stay as whole groups for a laptop. Artifact names stay `ci-<os>-<group>-attempt-N`.

Expected from the step timings: windows go-test from 504 s to about 270 s a job, the board from 518 s to about 270,
macos board from 552 to about 290, so the run's wall goes from 12 to 14 minutes to about 6 to 7. Local run of
`--group go-test-1` on a loaded 20 core box: 2235 tests through the merged stream, counts and time guard reading it.

## Test plan

## @LETTER@. CI runs the suite in halves

### @LETTER@1. The groups

1. Run `bash scripts/ci.sh --list-groups`.

**Expected:** go-test, go-test-1, go-test-2, go-checks, board, board-1, board-2 and board-visual, with go-test-1 and
go-test-2 both `go-build,go-test,time-guard,clean-tree` and board-1 and board-2 both `board,clean-tree`.

### @LETTER@2. The board halves partition the gating units

1. Extract `board_units` from ci.sh and run `board_units gating`, `gating-1` and `gating-2`.

**Expected:** the sorted union of the halves equals the gating list, no unit is in both, core, history and core2 are
all in one half, and a second call gives the same answer.

### @LETTER@3. A go-test half

1. Run `CI_ALLOW_TOOLCHAIN=1 bash scripts/ci.sh --group go-test-1`.

**Expected:** a `part 1: daemon -run '^Test[A-F]', then: ...` line, two reporters running at once, one
`build.claude/ci/go-test.json` with both streams, `go-test.report.txt`, and time-guard reading it.

### @LETTER@4. On a push

1. Push a branch and open the run.

**Expected:** eighteen jobs, six per platform. The skins, contrast, opencode, release and parse steps say
`skipped: runs once, on the linux job.` on macos and windows go-checks and run on ubuntu. The slowest job is near half
the previous slowest.
