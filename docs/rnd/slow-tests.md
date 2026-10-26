# Why the slowest daemon, api and board tests are slow

Read-only analysis for `q-slow-tests`. Nothing was fixed. Times are `go test -tags integration -count=1 -v` on one
darwin/arm64 laptop, one run each, run in parallel as CI runs them. A test's own time includes waiting for its
`t.Parallel()` peers to free a CPU, so a figure here is an upper bound on what the test does alone.

## Totals

| Package | Wall | Tests | Sum of test times | Tests at 0.3s or more |
| --- | --- | --- | --- | --- |
| `internal/daemon` | 198.5s | 1905 | 398.5s | 484, holding 341.8s |
| `internal/api` | 62.3s | 391 | 310.8s | 243, holding 302.5s |

Without `-tags integration` the daemon package runs in 0.5s. Every cost below is in an integration test.

## Daemon: the slowest

| Test | Time | Why | Smallest fix |
| --- | --- | --- | --- |
| `TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize` | 7.13s | The child prints AAA, sleeps 6s, prints BBB, and the test waits for BBB after two reattaches (`hostterm_integration_test.go:217`) | Shorten the child's 6s pause to 2s, as `ec6fa2ac2` did for the replay test. Saves about 4s |
| `TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated` | 5.06s | Two 2s pauses between three lines, waits for the third | Already cut by `ec6fa2ac2`. Nothing more without losing the ordering it proves |
| `TestAClearMarkSurvivesAReadoptOfAPtyHostTerminal` | 4.95s | The child pauses 4s before BBB (`hostmark_integration_test.go:26`), plus a flat 500ms `time.Sleep` at line 37 | Pause 2s, and wait on the condition instead of the 500ms. Saves about 2.5s |
| `TestRestartRunnerLeavesAStubbornRunnerUp` | 4.02s | Starts a real daemon and a real slow runner, then asks it to exit with a 300ms grace | Cause of the 4s not traced. Read it with `-v` before touching |
| `TestAStartWritesItsRunRowBeforeTheLaunchIsComplete` | 3.60s | `run_exit_integration_test.go:228` loops on a flat 500ms `time.Sleep` with a 10s ceiling | Poll at 25ms like `hostWait`. Saves up to 0.5s per pass |
| `TestARunnerAndAShellOnOneCardHaveTheirOwnRuns`, `TestARunnerThatListsTheFlagTakesIt`, `TestStopRunnersEndsHostRunners`, `TestAnOlderClaudeIsStartedWithoutTheFlagAndTheDetailsSayWhy` | 3.2s to 3.3s each | Each starts a real pty host and a real child | Cause not traced |

The rest of the 1905 are under 3s. The package is 198s wall because 484 tests are at 0.3s or more and each starts a
daemon, a database and often a pty host. The biggest single lines are the pauses inside a child's script: the first
three rows hold about 10s of them that only set the order of output.

Zrok and Ziti save tests sit at 0.34s to 0.40s each (`TestZrokPublicSavesWithALogin`, `TestZitiRefusesWithoutWhatItNeeds`
and six peers). That is a steady 0.34s, not a wait. Not traced further.

## Api: the slowest

| Test | Time | Why | Smallest fix |
| --- | --- | --- | --- |
| `TestListTasksLauncherCostWithManyCards` | 3.59s | Registers 300 cards, then times `/v1/tasks` five times (`launcherid_integration_test.go:100`). It logs a cost and asserts nothing about speed | Cut to 100 cards, or make it a benchmark. Saves about 2.5s, not measured |
| `TestAGroupingExpressionCannotBeStoredOnTheDaemon` | 2.56s | Calls `fileServer(t)` once per field, each opening a fresh store. The assertions are instant | Build the server once outside the loop. Saves about half |
| `TestCloseSaysEachStepInItsOwnOrderAndTicksThemOff`, `TestCloseRemovesAnIgnoredDeepTreeAndCountsItsBytes`, `TestCloseDeletesWhenToldTo`, `TestCloseFreesWhatTheOpenMadeAndKeepsTheHistory` | 1.9s to 2.4s | Not traced | Not proven |
| `TestImportBroadMCPDenyNeedsInclude`, `TestPRWorktreeRaisesAndClearsTheForgeAlert`, `TestTaskEventFromAClosedStoreSendsNothing`, `TestPublishTaskCoalescesABurstToTheLatestState` | 1.9s to 2.3s | Not traced | Not proven |

The api package is flat. 243 of 391 tests take 0.3s or more and the 15 slowest are all between 1.2s and 3.6s. The
common start is `fileServer` (`store.Open` on a temp database, then `New(st)`), so the 62s looks like many roughly
1s tests over a few CPUs, not a handful of waits. I did not profile `store.Open`, so that is a reading, not a finding.

## Board units

Not run here, the room does not run board units. These are the recorded weights in `scripts/board-suite-weights.json`
(milliseconds per unit in the last sharded run), not a fresh measurement. The brief's 59.5s is not in that file, whose
largest is 48.0s.

| Unit | Time | Why | Smallest fix |
| --- | --- | --- | --- |
| `mChangeReq` | 48.0s | Not traced. `board-tests/changereq.js` holds no flat waits, so it waits on real work | Read a TIMING run of the unit |
| `changeReq` | 32.5s | As above | As above |
| `core2` | 25.8s | Not traced | Not proven |
| `peekEverywhere` | 24.9s | Not traced | Not proven |
| `mOlder` | 24.7s | Not traced | Not proven |
| `mFollow` | 24.4s | Not traced | Not proven |
| `atriumDown` | 21.5s | Not traced | Not proven |
| `usageGroups` | 20.4s | Not traced | Not proven |

Flat waits are not the cause for the top unit, but 821 `waitForTimeout` or `setTimeout` lines without a `wait:` mark
sit in `scripts/board-tests` and `test-board-headless.js`. The largest files are `usage.js` (29), `rooms.js` (21),
`settings.js` (18) and `chat.js` (16), counted per file with marked lines included. They are the first place to look for seconds.

## What is proven and what is not

- Proven by running: the totals, every per-test time above, and the 0.5s of the daemon package without the tag.
- Read from code, not timed after a change: every "saves" figure.
- Not traced: every row that says so. A fix there needs a profile first.
- Nothing was fixed or changed by this report.
