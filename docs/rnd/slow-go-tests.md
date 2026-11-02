# Why the slowest daemon and api Go tests are slow

Status: research, item q-slow-tests. One test-side fix is built (below). Board units are out of scope here.

The item's numbers (internal/daemon 216.8s, internal/api 64.1s) are the integration runs. Without the tag the two
packages take 3.8s and 2.0s.

## How this was measured

`go test -json -count=1 -tags integration` on `internal/daemon` and `internal/api`, side by side, on a 28 thread
Windows worker. The stream was split at the first `cont` event: Go runs every serial top-level test first, then
releases the `t.Parallel()` tests together, so a package's wall time is its serial phase plus its parallel phase.
Single tests were then run alone with `-parallel 1` to separate their own cost from load.

THIS WORKER'S SANDBOX REFUSES `taskkill`. Every call takes 5.1s and answers "Access denied" (measured on a child
process and on a pid that does not exist). Any test that stops a process through `cardproc.StopTree` pays 5s per call
here that it does not pay on a normal host. Those tests are marked "sandbox" below and their seconds here are not a
finding.

| package | wall | build | serial phase | parallel phase | serial tests | parallel tests |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| internal/daemon | 231s | about 24s | 126s | 102s | 571 | 1332 |
| internal/api | 85s | about 24s | 52s | 28s | 94 | 301 |

The serial phase is where the wall time goes. A parallel test only sets the wall when it is the last one running, and
the slowest parallel test (11s) is far under each parallel phase, so shortening one saves its own seconds and next to
none of the package's.

## internal/daemon

### Serial phase (126s)

| test | here | alone | what it waits on | fix | saves |
| --- | ---: | ---: | --- | --- | ---: |
| TestAStoppedSourceEndsWhatItStarted | 20.1s, failed | | the grandchild to be gone after the source is cancelled. The kill is refused (sandbox), so it rides out `waitFor`'s 10s twice | none, sandbox | 0 on a normal host |
| TestALaunchedClaudeStartsOnTheClassicRenderer | 10.1s, failed | 1.2s after the fix | `env.txt` under a 10s deadline. Launch was refused because no Git Bash was found, so the file never came | BUILT, below | 9s wherever Git Bash is missing |
| TestNoAckNeverClears | 2.4s | 2.5s | a negative check: it holds for `captureEnd + clearWait` to prove no `/clear` is typed | none, the wait is the assertion | 0 |
| TestIdleParkRule | 2.4s | 2.5s | real runners parked on the idle timer | none test-side | 0 |
| TestChangesTurnWithHundredsOfPathsFitsTheCommandLine | 2.4s | 1.9s | git over hundreds of paths | none test-side | 0 |
| TestALaunchKeyReturnsTheCardAlreadyFiledUnderIt | 2.3s | 2.3s | real launches and stops | none test-side | 0 |
| TestAnInterviewTagSurvivesEveryLaunchOntoTheCard | 2.1s | 2.1s | four subtests, each a daemon plus one real runner start and stop at about 0.5s | none test-side | 0 |
| TestRestartRunnerLeavesAStubbornRunnerUp, TestConcurrentRestartsKeepOneRunner, TestAWokenCardThatNeverTakesItsMessagesTellsTheLauncher, TestTheCycleWaitsForTheHumansLineAndQuiet | 1.8 to 2.1s each | the same | real runners and the new-context cycle's timers | none test-side | 0 |

After the top two, the serial phase is a long tail: 571 tests, the slowest file (`changes_integration_test.go`) is
11s over 15 tests, and no other file passes 7s. The cost is spread over starting daemons, shells and git, not over
sleeps. `docs/changes/t-daemon-tests-slow.md` lists why each of these stays serial (environment, working directory,
package seams, log redirection, goroutine stacks, and timing tests that flaked under load).

### Parallel phase (102s)

| test | here | alone | what it waits on | saves |
| --- | ---: | ---: | --- | ---: |
| TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated | 11.2s | 8.5s | its own script: `echo AAA; sleep 4; echo BBB; sleep 4; echo CCC`. The 8s of sleeps leave room for the restart between the lines | about 7s of its own, 0 of the wall |
| TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize | 9.3s | 6.8s | the same, `sleep 6` between AAA and BBB | about 5s of its own, 0 of the wall |
| TestAClearMarkSurvivesAReadoptOfAPtyHostTerminal | 7.1s | 4.5s | the same, `sleep 4`, plus a marked 500ms settle | about 3s of its own, 0 of the wall |
| TestAnUnreachableHostFallsBackToAnInProcessTerminal, TestARunnerThatLandsInProcessWithTheSettingOnIsSaidOnItsCard, TestARunnerLaunchedAfterAFailedDialStillGoesToTheHostOnceItIsUp | 8.6s each | 5.5 to 5.8s | an in-process runner's `Kill`, which calls `killTree`, which is `taskkill` (sandbox, 5s). `CloseShell` adds up to two `shellHangup` (1s) waits on a shell that does not end on close | 0 on a normal host |
| TestAKilledCardStaysDown, TestStopRunnersEndsHostRunners | 5.6s, 5.0s | 5.6s, 2.8s | kill paths through `taskkill` (sandbox) | 0 on a normal host |
| TestPRFetchSourceTimedAgainstALocalRemote | 4.9s | 2.0s | git fetch against a local remote, slowed by load | 0 |

The three reattach tests could gate their scripts on a file the test creates after the restart, instead of a fixed
sleep. That needs one script for `sh` and one for `pwsh` (`someShell` picks pwsh on Windows), and it saves no package
wall time, so it is not done here.

Every other parallel test runs at 0.1 to 0.5s alone and 2 to 4s here: 28 at once, each starting a daemon, a store
and processes, share the machine. That inflation is load, not a wait.

## internal/api

### Serial phase (52s)

| test | here | what it waits on | saves |
| --- | ---: | --- | ---: |
| TestCloseStopsAProcessTheRoomStartedForTheCard | 11.7s, failed | two `taskkill` calls on close (sandbox) | 0 on a normal host |
| TestCloseStopsAndDeletesTheCardsOverlay, TestOverlayDownStopsOnlyThatOne, TestCloseStopsAProcessTheCardOwns | 5.2 to 5.6s each, failed | one `taskkill` each (sandbox) | 0 on a normal host |
| TestNewWork* (seven tests) | 0.8 to 2.1s each | real git: worktrees, forks, fetches and the setup hook | 0 test-side |
| TestOpen* (six tests) | 0.5 to 1.8s each | the same, through the open route | 0 test-side |

The four kill tests are 27.7s of the 52s here and about 1s on a host where `taskkill` works.

### Parallel phase (28s)

196 parallel tests take 2.4 to 4.5s each in the full run. Twelve picked at random take 0.09 to 1.25s alone
(`TestBrowseRefusesTraversal` 0.09s, `TestTaskEventEqualsListRow` 0.60s,
`TestCloseStashesToTheHubAndKeepsTheWorktreeWhenTheStashFails` 1.25s). The fixture is not it: `fileServer` plus `cardIn` is 73ms, of which `store.Open` is about 70ms (migrations
on a fresh file). The rest is load from about 28 tests at once.

## Built

`TestALaunchedClaudeStartsOnTheClassicRenderer` set `CLAUDE_CODE_GIT_BASH_PATH` with `t.Setenv`. The launch strips
every inherited `CLAUDE_CODE_` variable (`runnersetup.InheritedTaint`), so the stand-in `bash.exe` never reached the
Git Bash check, and the test passed only on a host with a real Git Bash. Elsewhere Launch was refused, the test
ignores Launch's error (the helper exits at once, which Launch also reports), and it waited out its 10s deadline.
The path now goes in the harness's `Env`, which is applied after the filter. Proven here: failed in 10.2s before,
passes in 1.2s after.

`TestASuperleanLaunchDeliversItsPrompt` and `TestALaunchWhosePromptAnOptionWouldTakeSaysSo` have the same host
dependence on Git Bash but fail at once, so they cost no time. Not changed.

## Needs a product change, not made

- None found for the daemon or api seconds above. The serial tail is the next lever, and
  `docs/changes/t-daemon-tests-slow.md` already names it: an in-process fake for the git and pty host fixtures, so
  those tests can run in parallel without the seams that keep them serial.
- `cardproc.StopTree` has a 20s context and nothing shorter. Where `taskkill` hangs or is refused, every close or kill
  pays its full time. That is a host problem, not a test one. It is listed so a slow kill test is read as the host
  first.
