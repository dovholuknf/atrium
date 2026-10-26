# Load-related flaky tests: classes, and the red runs a pass-under-load rule would have saved

Item `q-load-eval`. Source: `scripts/flaky-jobs.jsonl`, 116 main runs from 2026-10-21 10:08Z to 2026-10-25 12:36Z, 36
of them red, and each failed job's log and `build.claude/ci` artifact read through the hub (`atrium_ci log` and
`artifact`, the same data as `gh run view --log-failed`). The flaky list `scripts/flaky-suite.json` is empty today, and
`scripts/flaky-history.md` holds its seeds.

## How a failure was judged load-only

- A board unit that fails in the shards is run once more, alone. `FAILED IN THE SHARDS, PASSED ALONE` (or `PASSED ON
  RETRY`) is direct evidence of a load-only failure. `failed both tries` or a `FAIL:` line in `retry-*.log` is not.
- A Go test gets no retry, so its class comes from its first error in `failing.tsv` and from its source. A wait that ran
  past its bound, a hang, or a file held by another handle reads as load. A wrong value with no clock in sight does not.
- A run with a lint failure, an unnamed failed step or a test that fails alone stays red under any rule.

## Classes

- **performance**: a fixed timeout or a time bound the test sets itself. A test problem.
- **concurrency**: load exposed an ordering the code or the test does not enforce. Maybe a real defect.
- **ui timing**: rendering or scheduling under a loaded browser. A brittle test.
- **not load**: fails alone, on every OS of a run, or on every run of one sha. A pass rule must not hide it.

## Load-related tests (19)

| Test | Runs | Class | First error, and what it says |
| --- | --- | --- | --- |
| `TestARunnerThatLandsInProcessWithTheSettingOnIsSaidOnItsCard` | 1 | performance | "the wait for a host that never comes ran 16.2s past its bound". Windows, a fixed bound. |
| `idleRate` | 1 | performance | "still had fetches out 5s after the room answered". Passed alone. |
| `TestStartRunsADetachedHostFromACopy` | 1 | performance | From source: a real process, a 20 s dial and a 15 s wait. Its run is red anyway. |
| `TestAHubPushesChangedPathsToAnAttachedRoom` | 1 | performance | From source: a 100 ms beat and a 1 s silence limit. Its run is red anyway. |
| `TestAStreamReopenedRightAfterACloseStillStreams` | 1 | concurrency | "no task event arrived" after 30 s. A lost event, the likeliest real defect here. |
| `TestADroppedCallerLosesItsStreamsAndOthersKeepThem` | 1 | concurrency | Windows rename of `remote_access.json` refused, Access denied. A handle still open. |
| `TestRestartRunnerOnAMissingCardSaysSo` | 1 | concurrency | "the daemon's Run had not returned 30s after it was cancelled". A shutdown hang. |
| `TestASeededMainFollowsItsForge` | 1 | concurrency | Fetch served the old main right after the cache was marked stale. Inferred from source. |
| `TestAnOlderClaudeIsStartedWithoutTheFlagAndTheDetailsSayWhy` | 1 | concurrency | The `--help` probe failed with no output and the flag was assumed. Inferred: ETXTBSY from a parallel fork. |
| `TestStartOutsideJobSaysWhenCmdCannotOpenAHeldLog` | 1 | concurrency | "PING.EXE ... had ended before it could be held". A race in the test's fixture. |
| `mCompose` | 1 | concurrency | "card two shows card one's draft". Passed alone. State that crosses cards, worth a look. |
| `growlOnIt` | 2 | concurrency | Focus and pop-out attach. Passed alone once, failed both tries alone in a flaky pass once. |
| `mPictures` | 1 | ui timing | `page.tap` 5 s timeout at 99% cpu. Passed alone. |
| `chatter` | 1 | ui timing | `waitForSelector` 8 s timeout at 94% cpu. Passed alone. |
| `unifiedCard` | 1 | ui timing | "done draws 5 cards, want 6 to 12". Passed alone. |
| `atriumDown` | 1 | ui timing | "did not put the restart cover back" after a reload. Passed alone. |
| `u016` | 1 | ui timing | A long press read as a paste. Passed alone. |
| `termFitHeight` | 2 | ui timing | "the last row ends at 830.5 but the hint bar starts at 846.3". Passed alone both times. |
| `growlPopout` | 1 | ui timing | "still thinks the card is popped out after the window closed". Passed alone. |

## Not load

| Test | Why |
| --- | --- |
| `cardViewBarInline` | Failed alone in 4 runs over 3 shas, the same layout numbers each time. A regression. |
| `controlsCompact`, `notifyTrayWrap`, `trayToast`, `land`, `cacheChip`, `phoneNudge` | Failed again in the alone retry. |
| `hears`, `cycleChip`, `mCardUpload` | Failed on all three OSes in one run. Not opened. |
| `phoneFocus` and six more `phone*`, `heldLine`, `terminalsOneLine` | One regression on one sha. |
| `TestEveryExecStartIsHiddenOrListed`, `TestADeniedAncestor...` (2), `TestOpenATicketWithNoRepo...` (2) | No clock. |
| `TestGuardHookBuiltin`, `check: titles`, `check: check-test-buckets` | Every OS, or lint. |
| `(step failed, no test named in the log)` | 20 jobs with no name to classify. |

## Count

A pass-under-load rule here means a red run turns green when every failure in it is one of the three load classes.

| Evidence | Runs saved | Running total | Of 36 red |
| --- | --- | --- | --- |
| Board units that passed alone | 9 | 9 | 25% |
| Go tests whose first error is a bound, a hang or a held file | 5 | 14 | 39% |
| Go tests classed concurrency from source only | 2 | 16 | 44% |

- Passed alone: 39704976949, 39648314162, 39715013378, 39721269229, 39722221154, 39732458356, 39735289836,
  39816184242, 39836241253.
- Go, from the error: 39718988038, 39729860399, 39810943290, 39814909117, 39704005124.
- Go, from source: 39618825311, 39952983819.

The other 20 held a lint failure, a test that failed alone, a deterministic Go test, the phone regression or a step
with no name. Run 39766570536 had only `growlOnIt`, but it failed both tries alone, so it stays red.

## Read it with care

- 8 of the 16 saved runs failed on a concurrency test. Five of those are possible defects: the lost stream event, the
  shutdown hang, the held file on Windows, the stale forge fetch and `mCompose`'s draft that crossed cards. A rule that
  passes them hides them, so a pass should still file the test.
- Only the 9 board runs are proven by a rerun. A Go test gets no retry today, so its 7 runs rest on reading the error.
  A one-shot alone rerun for Go, as the board has, would turn them into evidence.
- The saved runs cluster on 2026-10-21 and 2026-10-22, when the macOS board shards ran at 92 to 99% cpu. No run after
  2026-10-22 19:00 is saved but one, and that one rests on source alone.
- 20 red jobs name no test. Naming the failing test there comes before any rule.
