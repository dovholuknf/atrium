# t-daemon-tests-slow

The internal/daemon package has about 1850 tests (with `-tags integration`) and none ran in parallel. CI windows hit
go test's 10m default at 550 to 600s. The cost is spent starting processes (git, shells, node, pty hosts) and
daemons, and not in sleeps, so the lever was `t.Parallel()` and not shorter waits. No sleep was cut. No production
code changed and the 20m timeout in scripts/ci.sh is untouched.

## What changed

- 1269 top-level tests, plus the xterm differential cases in `TestScreenAgainstXterm`, call `t.Parallel()`.
- A test stays serial when it, or a helper it calls, sets the environment, changes directory, assigns a package
  level seam (`readUserSettings`, `stopAfterReport`, `modelRetryEvery` and the rest), redirects `log`, measures
  allocations or memory, reads every goroutine's stack, or touches a shared cache or counter.
- The conpty, input lag, idle tick and scroll repro files, the new context wedged test and the restart wake tests
  stay serial because they are timing sensitive and flaked under load.

## Numbers (sg3, Windows, 12 cores, `go test -tags integration -count=1 ./internal/daemon/...`)

| | wall seconds |
| --- | --- |
| Before | 511 (package 504, sum of test times 501) |
| After, run 1 | 207 |
| After, run 2 | 207 |
| After, run 3 | 200 |

Slowest before: TestScreenAgainstXterm 15.4, TestChangesTurnWithHundredsOfPathsFitsTheCommandLine 12.5,
TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated 9.3, TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize
7.7, TestLaunchKeepsTheKitOnTheCardForARestartAndWritesNothingInTheWorktree 6.7, TestChangesBoundsCutTheListAndSayWhat
5.6, TestAClearMarkSurvivesAReadoptOfAPtyHostTerminal 5.3, TestChangesBaseIsTheBranchSinceTheMergeBase 4.2 and then
2 to 4 seconds each for the cull, merged, keepalive and attach tests.

Slowest after (run 3): TestChangesTurnWithHundredsOfPathsFitsTheCommandLine 11.2, TestReattachReplays... 9.7,
TestAResizeAfterReattach... 7.7, TestAClearMarkSurvives... 5.6, TestChangesBoundsCut... 5.4 and the same 2 to 4 second
cull and attach tests. Each is a single serial git or pty chain, so they now set the floor. About 200s of the 500s
before was serial tests that still run one after another, and that is the next target (an in-process fake for the
git and pty host fixtures).

Flakes: an intermediate run that also parallelised the keepalive subtests and the newcontext wedged and restart wake
tests failed `TestAWedgedWaitForTheAckIsNeverATrap` and `TestAWakeSurvivesTheDaemonComingBack` (SQLITE_BUSY under
load). Those were backed out. The three final runs have no failure beyond the known ones below.

Failing both before and after on sg3, not touched: TestCaptureEnvKeepsWhatThePrepareCommandSet,
TestCaptureEnvKeepsPathOrder, TestCaptureEnvIgnoresWhatThePrepareCommandPrinted. TestALinkCardIsNotExitedByItsReport
failed once in the before run and passed in all after runs.

## Test plan

## @LETTER@. The daemon tests run in parallel and stay green

### @LETTER@1. Package time

1. Run `go test -tags integration -count=1 ./internal/daemon/...` on a Windows machine.

**Expected:** the package finishes in well under half of the old 500s plus, with no failure beyond the known list.

### @LETTER@2. No new flakes

1. Run the same command three times in a row.

**Expected:** the same tests pass each time. A test that fails only in a parallel run is a shared global that needs the
test moved back to serial.
