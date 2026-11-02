# Timing for one merged non-flaky Go test pool (stage 1)

Read-only report for backlog item `q-pool-timing-1`. No product code, test or script changed. Go tests only, because this
room does not run board units. The CI-sourced report `docs/qa/pool-timing.md` covers windows and the board, and this one
is the same pool measured on this room.

Measured on this room (darwin/arm64, 8 cores, go1.26.10) on claude/main at ca6a5c879:

    bash scripts/test.sh full -json -count=1 -timeout 20m -p 8

`full` is the tag set CI runs (`integration,git`). `scripts/flaky-suite.json` has no entries, so every test that ran is
in the non-flaky pool. One run, no failures, 3 min 43 s wall, 67 packages, 5256 top-level tests (42 skipped). Times are
`go test -json` elapsed seconds. A package's time is its test binary's wall time while other packages ran beside it, so
it includes contention at `-p 8`. A single run, so expect a few seconds of noise on the small numbers.

## Packages

The 12 packages over 4 s hold 98 percent of the 620 package seconds. The other 55 hold 11.9 s together, none over 3.5 s.
"Test s" is the sum of the top-level tests' own times, which exceeds wall where tests run in parallel.

| package | wall s | tests | test s |
| --- | --- | --- | --- |
| internal/daemon | 204.4 | 1908 | 399.5 |
| internal/gitsync | 106.0 | 286 | 299.5 |
| internal/link | 84.0 | 927 | 261.0 |
| internal/api | 64.0 | 391 | 318.7 |
| internal/cli | 32.3 | 326 | 31.0 |
| internal/store | 27.7 | 468 | 78.9 |
| internal/hubstore | 19.5 | 226 | 18.1 |
| internal/ptyhost | 16.4 | 26 | 16.2 |
| internal/link/conn | 10.1 | 85 | 48.7 |
| internal/worktrees | 6.7 | 12 | 6.5 |
| internal/roomstats | 5.0 | 22 | 4.6 |
| internal/deployready | 4.9 | 37 | 26.7 |
| internal/cardreview | 4.6 | 8 | 3.9 |
| internal/runnersetup | 4.4 | 36 | 4.2 |
| 53 more packages | 11.9 | | |

## Tests

3660 tests take under 0.1 s, 778 take 0.1 to 0.5 s, 228 take 0.5 to 1 s, 429 take 1 to 2 s, 148 take 2 to 5 s and 13
take 5 s or more. The 590 tests at 1 s or more hold 1090 of the 1521 test seconds. The slowest:

| s | package | test |
| --- | --- | --- |
| 9.14 | store | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| 9.01 | daemon | TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated |
| 8.99 | ptyhost | TestStartRunsADetachedHostFromACopy |
| 8.01 | cli | TestEveryHookFailsOpenInsideItsBudget |
| 7.86 | link/conn | TestNothingIsSentWhenNothingChanged |
| 7.66 | link | TestMergeLandPutsOneCommitOnMainAndLeavesClaudeMain |
| 7.52 | store | TestOpeningTheSameStoreTogetherNeverFailsMidMigration |
| 7.05 | daemon | TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize |
| 6.69 | link | TestAHungRoomDoesNotStallThePrune |
| 6.01 | link/conn | TestAnAnswerWithNoCardListIsNotAnEmptyRoom |
| 6.01 | link/conn | TestARoomInTroubleDoesNotAnnounceItselfEmpty |

## Reading it for a split

- The pool is 620 package seconds. Four equal jobs hold about 155 s each, and the same package walls say eight hold
  about 78 s.
- daemon is a third of the pool alone (204 s) and has to be cut by test name to balance, as `scripts/ci.sh` already does
  with `^Test[A-F]`. gitsync (106 s) and link (84 s) fit whole in a job of 155 s or less. Below about 100 s per job
  gitsync is the floor unless it is cut by test name too.
- Most of a package's time is a few hundred tests of 1 s or more, so a cut by test name balances more finely than a cut
  by package.
- On windows these packages are 2 to 4 times slower (gitsync 416.6 s, link 331.2 s, daemon about 490 s per the CI
  report), so size a windows job from the CI numbers and this table gives only the shape.
- The windows `rest` list in `scripts/ci.sh` runs internal/api, internal/cli and internal/daemon twice, found in the CI
  report section 2. Fixing it comes before any rebalance.
