# t-tests-fast: every Go package measured, the slow ones made fast

Steps 1 and 2 of the item. Steps 3 and 4 (the short/tag split and the CI guard) are not part of this change.

## Measured

Every package ran alone with `go test -tags integration -count=1 -json`, one package per call, on a Windows box that
another worker was loading with the daemon tests at the same time. Numbers are wall seconds for the package and move
by 20 percent or more between runs for that reason. Packages not listed ran in 3s or less (cardcolors, cardproc,
cardurl, edge, forge, inputlag, itemgate, linkfetch, nowindow, prreview/render, requirements, resources, roomspec,
roomstats, runnerprofile, safepath, shellpick, testdiag, webasset, scripts/recognisers, and the three with no tests).

| Package | Before | After | Note |
| --- | --- | --- | --- |
| internal/gitsync | 189 | 189 | not changed here, made parallel in 54d9db5c, ran loaded |
| internal/api | 68 to 127 | 57 to 74 | see below |
| internal/store | 68 | 40 | see below |
| internal/deployready | 55 | 14 | see below |
| internal/cli | 28 | 28 | under 60s, left |
| internal/hubstore | 15 | 15 | under 60s, left |
| internal/ptyhost | 11 | 11 | under 60s, left |
| internal/runnersetup | 6 | 6 | left |
| internal/link | owed elsewhere | | 407s to 238s tonight |
| internal/daemon | owed elsewhere | | another worker has it |

### Tests over 1s, with the cause

- gitsync (189s): thirty tests of 16 to 50s each, every one a real git subprocess rig (bare hub, clones, pushes).
  Owned elsewhere. Not changed.
- api: about 30 tests of 1.4 to 2.4s, all git subprocess. Each built its own upstream repo with eight git runs.
  TestBoardScriptParses 5s is a real parse of the whole board script (CPU). All 308 tests were serial.
- store: no test over 1.5s. 438 tests at about 0.14s each, each opening and migrating its own database, all serial.
  The two over 1s are TestMCPPrecedence and TestAReportFailingBetweenWritesLeavesNothing.
- deployready: every one of 36 tests is 1.1 to 3.2s, a git repo built with a run of git subprocesses. All serial.
- cli: TestRunnerPIDFindsOpencode and TestFakeRunnerSubagentsDoNotEndTheTurn 2.8s (real child processes),
  TestFakeRunnerDoneSayRidesTheStopHook, TestFakeRunnerContinuedTurnStillEnds,
  TestFakeRunnerDrivesTheCardThroughEveryHook 1.5 to 1.9s (fake runner process),
  TestPermProbeNotGatedAndTimeout 1.0s (a real timeout).
- hubstore: TestOldSnapshotsThinOutRatherThanPilingUp 5.8s writes 8640 small files. Left.
- ptyhost: TestIdleExit 3.9s (a real idle timer, the sleep is the assertion), TestExitWithNoClientIsKeptAndCollected
  1.5s (real pty). Left.
- runnersetup: TestConcurrentClaudeWritesKeepEveryChange 3.7s (real concurrent file writes). Left.
- roomstats: TestLoadedProcessDriftsMore 1.3s (real sampling). Left.

## Changed

- store: `t.Parallel()` on 391 tests. Left serial: the files that swap the package clock `now` or `failpointHook`
  (dispatch, heldnotices_race, replay, seen, ledger). Go runs every serial test to the end before it resumes the
  parallel ones, so the swaps are never seen by a parallel test. No test uses t.Setenv or Chdir.
- api: `t.Parallel()` on 257 tests. Left serial: files that assign package hooks (`excludedPorts`, `RunnerCompactAtK`,
  `HeldNoticesOf`, `OwedOf`, `CheckLeanGateway`, `PRArchiveCap`, `LiveRings`, `IsSupervised`), files that call
  t.Setenv through `setupServer` or directly (openkinds, runnersetup, roomstats), and the overlay and port files.
  `upstream()` builds the fixture repo once per package and each test copies it, because some tests add branches to it.
  Its template folder is removed in TestMain.
- deployready: `t.Parallel()` on 33 tests. TestSlowGitEndsAtTheContextBoundAsUnknown stays serial since it asserts
  a wall time.

No test was deleted and no assertion weakened. Each changed package ran twice in a row, both green apart from the
failures below.

## Left over, and why

- gitsync stays over 60s. It is owned elsewhere. link and daemon likewise.
- api is near 60s on this loaded box. Three tests fail here and not on a normal box, and they cost about 16s of the
  wall time by themselves: TestCloseStopsAndDeletesTheCardsOverlay, TestOverlayDownStopsOnlyThatOne and
  TestCloseStopsAProcessTheCardOwns wait out a 5s retry on `taskkill: Access denied`. The sandbox this ran in forbids
  killing a process. Without them api is about 45s.
- Failures on this machine that are the machine and not the code: claudeconf (TestInstallWritesThroughASymlink,
  no symlink right on this Windows account), and every git-committing test fails when the global git config signs
  commits with a key this account lacks. The runs above set `commit.gpgsign=false` through GIT_CONFIG_COUNT. gitsync
  had 3 failures of the same kind (TestEpochMoveKeepsWhatLandedAfterClintFetched,
  TestEpochMoveToMainWhenEverythingIsAlreadyThere, TestItemFactsSeeThroughACherryPick).
- Decision: a per-test deadline was not added. Every package is under its `go test` timeout and a hang already fails
  the package. It belongs with step 3.

## Test plan

## @LETTER@. The Go tests are fast and still whole

### @LETTER@1. Slow packages run in parallel

1. Run `bash scripts/test.sh all ./internal/store/ ./internal/deployready/ ./internal/api/`.
2. Run it a second time.

**Expected:** both runs pass, store under 60s, deployready under 20s, and the same count of tests as before the change.
