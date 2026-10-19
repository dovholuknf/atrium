# QA lessons learned: flaky tests and the CI that runs them

What the 2026-10-18 push to a green main taught. It sits beside `docs/troubleshooting-tests.md`, which holds the worked
cases step by step. This page holds the rules and the evidence behind them. The ledger numbers come from
`scripts/ci-flake-ledger.js` (data in `D:\tmp\qa-flake-ledger\`, 57 runs by the end of the day).

## The numbers

- The first 15 ledger runs averaged about 10 failing rows each. After the board units moved to the fake clock the
  next runs ran at 5, 2, then 2, 1, 1, and the 12-job matrix ran green once.
- Of 104 distinct failing units, most failed once. A green run after a fix says little. A fix counts when it adds a
  wait on the state the step asserts, and it is judged over several runs, not one.
- Every run still found two to four units that had never failed before. A unit is NEW until it fails twice, so
  chase the class (a wall-clock wait) and not the single unit.

## Rules that held

1. **Read the log, trace the cause, then fix.** Every fix that stayed fixed named its cause from a failing log. A fix
   made from a theory came back. `termBox` had a tidy stale-draw story and the trace placed the draw elsewhere.
2. **Never widen a timeout.** A longer limit moves the failure to a slower runner. Find the event the test is waiting
   for and wait for it.
3. **No flat sleeps.** Poll in steps of 10 ms (local) or 50 ms (http) under one deadline. An absence check waits for
   a sentinel or moves a fake clock. `internal/testwait` is the Go helper.
4. **Pause the fake clock, then tick it.** `pauseAt(Date.now() + 60000)` fires timers instead of holding them.
   `fakePause(p)` in `scripts/lib/browser.js` is the fix, and `fakeTick` or `fakeRun` then moves time. A unit that
   sleeps on a real clock loses to a loaded runner. A unit on the fake clock does not (`pollsGone` went from 27 s to
   3 s).
5. **A wait on a count is not a wait on the event.** The drawer finger check waited for the page's touch count to rise.
   The count rose from another event and the drag still ended short. Wait for the event's own point (type and y).
6. **Measure, then act.** `"390px stack 5"` failed with the click point at y=445 and the target row at y=152 to 222.
   The page had reflowed between measuring the point and clicking it. Only the failure dump showed it. Re-measure
   after the layout settles, or click the element and not a stored point.
7. **A test fault is the usual answer, and the product is sometimes wrong.**
   `TestALaunchMakesTheItemAndAFailedStartEndsItOnce` was a product race: a runner that exited early beat the launch event and opened a second generation and a second
   notice. The test was strict and right. Decide from the log, and keep the test strict when the product is at fault.
8. **A slow-CI failure is often a skipped test doing work first.** `internal/roomspec` ran 123.9 s against a 120 s
   limit because a docker test pulled an image a Windows docker server could never run, then skipped. The skip now
   comes first. Read the per-test times in the artifact before touching the limit.
9. **Ports are not free after you pick them.** A test that closes a `:0` listener and then asks for a second port can
   get the same port twice, or lose one to another process. Hold both listeners open until both are chosen, and retry
   the bind. A discarded `Run` error hid the cause for 5 s of polling.
10. **A fix to the test's own pacing beats a wire test that depends on pacing.** Writing 70 bytes 10 ms apart against a
    300 ms idle limit fails on a loaded runner. The idle deadline is checked in a unit test that asserts every read
    moves it. The cost is that no wire test of a slow body remains, and the standard library enforces the deadline.

## Reading CI

- **Use `atrium_ci` only.** A room never runs `gh`. A run's logs refuse until the whole run is complete.
- **Read in this order:** the run's jobs and steps, the failing job's `failing.tsv`, `steps.tsv` and `summary.md`, then
  `board.log` and the shard logs. A job with an empty `failing.tsv` failed on a step, so read `steps.tsv`.
- **`failing.tsv` is not the list of what failed.** A check that is not a unit prints `FAIL` into `board.log` and never
  reaches the tsv. A `time-guard` failure is a step, not a test.
- **A green run can hide a failure.** `board-visual` is report-only, so a unit that failed there leaves the job green.
  The ledger reads it from the step's rows. Ingest green runs with `--conclusion success`.
- **A `@ci@hub` notice names a run with a SHA of `0123456` when it is a fixture.** Ignore those. A push starts one run
  of its own, so two manual runs plus the push make three.
- **Do not trust a summary of a log.** A minion's reading of a failure was wrong once: it said a click hit an element
  inside the row, and the dump showed the click point was outside the row's box. Read the dump yourself.

## Failure-time dumps

A check that fails once in 30 runs teaches nothing unless the failure carries its own cause. The drawer finger checks
and `1280px board 5` now print, only on failure, what was sent, what the page received and the drawer state after each
step, plus the click target, what covered it and the requests since the click. Rules for a dump:

- It prints only on failure and never changes a passing run's output or steps.
- It never starts with `FAIL `, so `ci.sh`'s `grep '^FAIL '` still files one row per check.
- Its own reads add latency. The drawer dump adds one `page.evaluate` per touch event, which can move the timing it
  records. A dump that makes the flake vanish is still a result, and the report should say so.

## The CI shape

- 18 jobs became 12. Per OS: ubuntu 3 (`go-all`, `board-1`, `board-2`), macOS 4 (`go-test`, `go-checks-visual`,
  `board-1`, `board-2`), windows 5 (`go-test-1`, `go-test-2`, `go-checks-visual`, `board-1`, `board-2`).
- The cause was macOS runner availability. In the 18-job run the macOS jobs started over about eight minutes. In the
  12-job run all 12 started within seven seconds and the run took 9 minutes against 16.
- `board-visual` cannot keep a job-level `continue-on-error` once it shares a job with `go-checks`. `ci.sh` keeps a
  `board-visual` failure out of its own exit status and leaves it in the log, the summary and `failing.tsv`.
- Balance by measured job time and not by group count. Windows `go-test` merged would run 12 to 14 minutes and set the
  run's length, so windows keeps the two Go halves.
- A job rename breaks a required status check that names the old job. Check branch protection when a group is renamed.

## Working with workers

- **One worker per failure, with the item filed first.** The brief names the failing run, the artifact, the method and
  the report format. A brief that says "if a first fix already landed, the diagnosis was incomplete" stops a second
  guess on top of a wrong first one.
- **Delegate CI reads to throwaway minions.** They download the artifacts, ingest the runs and report. The director
  reads the ledger report itself before saying anything.
- **Check each diff before merging.** Read the change, cherry-pick only the `+` commit from `git cherry claude/main
  <branch>`, reset the qa worktree to `claude/main`, and run `scripts/test-touched.ps1` against the SHA before.
  `test-touched` prints "nothing to run" when the worktree was not reset first.
- **Trim worker comments.** The repo keeps a comment only for a one-line why.
- **A land rewrites SHAs.** After `scripts/land-claude-main.ps1`, run `git cherry claude/main HEAD` in the qa worktree
  and reset to `claude/main`.
- **Load and repeat runs belong on the load machine.** A repeat loop on a shared room burns the CPU of everyone on it.

## Still open

- `phoneNudge` (9 failing runs of 57) has a diagnosed class and no approved fix.
- `step: time-guard` also failed on `internal/worktrees` `TestHeaderNamesTheRef` (12.92 s against 10 s) on windows.
- `390px stack 5` is traced to a layout shift and has no fix yet.
- `toastsTop` (macOS), `resumeSpinner` (windows) and `websiteSkin` (windows) still fail in the shards and pass alone.
- Flat sleeps remain in the J2 to J6 batches of the sleep-free program, and the lint that would stop new ones is not
  written.
