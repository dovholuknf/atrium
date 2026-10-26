# Load-related flaky tests: classes, and the red runs a pass-under-load rule would have saved

Item `q-load-eval`. Source: `scripts/flaky-jobs.jsonl`, 116 main runs from 2026-10-21 10:08Z to 2026-10-26, 36 of them
red. `gh` is not on PATH here and the store holds test names only, with no failure text, so nothing below was checked
against a CI log. Each class comes from reading the test, and a test I did not open is marked inferred.

## Classes

- **performance**: a fixed timeout or a time bound the test sets itself. A test problem.
- **concurrency**: load exposed an ordering the code or test does not enforce. Maybe a real defect.
- **ui timing**: rendering or scheduling under a loaded browser. A brittle test.
- **not load**: deterministic. It fails on every OS and every run of a commit, so a pass rule must not hide it.

| Test | Failed in | Class | Why |
| --- | --- | --- | --- |
| `TestStartRunsADetachedHostFromACopy` | 1 | performance | Starts a real process, then a fixed 20 s dial and a 15 s wait. |
| `TestARunnerThatLandsInProcessWithTheSettingOnIsSaidOnItsCard` | 1 | performance | A 150 ms host wait bounded by 10 s. |
| `TestStartOutsideJobSaysWhenCmdCannotOpenAHeldLog` | 1 | performance | Windows only, asserts the call took under 4 s. |
| `TestAHubPushesChangedPathsToAnAttachedRoom` | 1 | performance | 100 ms beat and a 1 s silence limit, a stalled runner drops the link. |
| `TestAStreamReopenedRightAfterACloseStillStreams` | 1 | concurrency | A reopen right after a close, with a 20 ms say loop. Worth a look as a defect. |
| `TestADroppedCallerLosesItsStreamsAndOthersKeepThem` | 1 | concurrency | A 100 ms beat and 5 s read deadlines around a teardown order. |
| `idleRate`, `termFitHeight`, `u016`, `cacheChip` | 1, 2, 1, 6 | ui timing, inferred | Terminal fit, frame rate and chip paint. |
| `cardViewBarInline`, `mPictures`, `controlsCompact`, `trayToast`, `growlOnIt`, `growlPopout`, `heldLine`, `notifyTrayWrap`, `cycleChip`, `terminalsOneLine`, `mCardUpload`, `mCompose`, `unifiedCard` | 1 to 6 each | ui timing, inferred | Layout and toast scheduling, passes alone. |
| `atriumDown`, `hears`, `land`, `chatter`, `phoneNudge` | 1 to 6 | ui timing, inferred | Could be a race in the board. Not opened, so lowest confidence. |
| `TestEveryExecStartIsHiddenOrListed` | 12 | not load | An AST scan with no clock. Trips when new code starts a process unhidden. |
| `TestADeniedAncestor...` (2), `TestOpenATicketWithNoRepo...` (2), `TestGuardHookBuiltin` | 3 to 4 | not load | No clock. Path, home or rule changes. |
| `TestASeededMainFollowsItsForge`, `TestRestartRunnerOnAMissingCardSaysSo`, `TestASayAfterARestart...`, `TestAnOlderClaudeIsStarted...` | 1 | unknown | No timing in the source. Not shown to be load. |
| `phoneFocus`, `phoneFollow`, `phoneKeyboard`, `phonePan`, `phoneShare`, `phoneTap`, `phoneView` | 6 each | not load | Seven units, two runs, one sha. One regression. |
| `check: titles`, `check: check-test-buckets` | 19, 6 | not load | Lint. |
| `(step failed, no test named in the log)` | 20 | unknown | No name to classify. |

## Count

A pass-under-load rule here means a run is green when every test it failed is a load class.

| Rule | Red runs saved | Of 36 red |
| --- | --- | --- |
| Only the six Go tests read above | 4 | 11% |
| Plus every inferred ui timing unit | 18 | 50% |

The 4: 39718988038, 39729860399, 39810943290, 39814909117. The 14 more are all single-unit or two-unit board runs.
The other 18 held a lint failure, a deterministic Go test, the phone regression, an unknown test or a failed step with no
name, and the rule would not have touched them.

## Read it with care

- The 50% is an upper bound. It treats 20 board units as load that I classed from their kind and not their body. A
  unit that is really a race counts as saved when it should stay red.
- The rule would also have hidden the two concurrency tests, the ones most likely to be defects.
- A run with no named failure (20 rows) can never be saved. Naming the failing test there comes before any rule.
- 80 of the 116 runs were green with no rule. The store starts at 2026-10-21, so older reds are not counted.
- To firm up the 18, rerun `ci-flake-ledger.js` on the ledger machine, which has the failure text and the retry
  columns, and mark each inferred row from its first-try error.
