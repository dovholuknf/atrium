# t-tests-fast step 3: a short set for a land

`make test-short` runs `bash scripts/test.sh all -short`, which is the full run (unit and integration tags) with every
test that guards itself with `if testing.Short() { t.Skip("slow: <cause>") }` left out. CI keeps running
`make test-all`, which runs everything.

## Before and after

Measured on one machine (8 cores, not the m1mini, so read the numbers as a ratio). Both runs were done with
`-count=1`, packages in parallel.

| run | wall time |
|---|---|
| `make test-all` before | 4m29s |
| `make test-short` before any skip | not run, same as above |
| `make test-short` after | 55s, then 56s on a second run |
| `make test-all` after | passes (one run hit the flake below, the next was clean) |

## What was skipped, and why a second tier exists

Skipping only the tests over 2s was not enough. The run had 202 of them, and skipping them left 3m12s, because the
daemon package has no `t.Parallel` anywhere and ~1,700 tests that each cost 100 to 500ms add up to over three minutes
on their own. The store, api and link tests were also inflated by running eight packages at once. So the skip went in
three passes, each driven by the measured times of the previous run:

1. Every top-level test over 2s in the first full run (the table below). 202 tests.
2. Every test still over 2s in the next short run, then every one over 1s. Tests that only looked slow because the
   machine was loaded were taken with them, since the cause is the same rig.
3. In `internal/daemon` only, every test over 200ms. The package is serial, so its wall time is the sum of its tests,
   and the package was the whole critical path.

778 tests carry the skip in total: gitsync 118, link 79, store 78, daemon 388, api 86, deployready 20, hubstore 2,
ptyhost 2, runnersetup 1, guard 1, cli 2. The cause string names the rig of the package: git subprocesses and a
smart-HTTP rig (gitsync), a link, hub and git rig on loopback (link), sqlite on disk (store), ptys, processes and
timers (daemon), a server with worktrees and a database (api).

No test was skipped because it is flaky. Subtests are not skipped on their own, the parent is.

## Resilience tests left in the short set

A test whose name mentions the halt, a hook, a shutdown or fail-open is never skipped, even when it is slow. The
repository has no AGENTS.md, so the list of guarantees comes from `docs/test-plan.md` section F and the halt notes in
`docs/archive/architecture-v2.md`. Two tests over 2s were left in because of it:

- `internal/store` `TestShareSurvivesAShutdownAndDiesOnAStop` (shutdown)
- `internal/gitsync` `TestARepositorysOwnHookAndConfigAreNotUsed` (a repository's hook is never run)

`TestHaltRefusesFurtherWork` in `internal/store` crossed 2s only under load and is also left in. The fixture-halt tests
in `internal/daemon` (`TestAFixtureFirstStartNeverHaltsTheStore` and its siblings) are fast and stay in too.

## A flake seen on the way

One full run failed `TestAWedgedWaitForTheAckIsNeverATrap` in `internal/daemon` (`store is closed` while a held message
was typed, 0.18s). The skip is a no-op outside `-short`, and the next full run passed, so this is a load race in the
test and not something this change touched. It is left alone.

## Tests over 2s in the first full run

Measured with `bash scripts/test.sh all`, wall time in ms, with eight packages competing for the machine.

| ms | package | test | cause | short |
|---:|---|---|---|---|
| 13040 | gitsync | TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch | git subprocesses, smart-HTTP rig | skipped |
| 12010 | link | TestAConnectionThatWaitedInThePoolStillWorks | link, hub and git rig on loopback | skipped |
| 10810 | store | TestMCPPrecedence | sqlite on disk | skipped |
| 9490 | gitsync | TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch | git subprocesses, smart-HTTP rig | skipped |
| 8200 | gitsync | TestPushToHubIgnoresAProxyTheClonesConfigSets | git subprocesses, smart-HTTP rig | skipped |
| 8104 | daemon | TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated | ptys, processes, timers | skipped |
| 6400 | gitsync | TestAnOperatorsPushedMainIsNeverOverwrittenByTheForge | git subprocesses, smart-HTTP rig | skipped |
| 6289 | gitsync | TestEpochMoveKeepsWhatLandedAfterClintFetched | git subprocesses, smart-HTTP rig | skipped |
| 6263 | link | TestAHeldAskIsPolledThroughAPause | link, hub and git rig on loopback | skipped |
| 6100 | daemon | TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize | ptys, processes, timers | skipped |
| 6070 | link | TestTheToolReturnsTheEndpointsJSONAndALine | link, hub and git rig on loopback | skipped |
| 6010 | link | TestAnAnswerWithNoCardListIsNotAnEmptyRoom | link, hub and git rig on loopback | skipped |
| 6010 | link | TestARoomInTroubleDoesNotAnnounceItselfEmpty | link, hub and git rig on loopback | skipped |
| 5310 | gitsync | TestAChunkedPostGoesThrough | git subprocesses, smart-HTTP rig | skipped |
| 5050 | gitsync | TestASeededMainFollowsItsForge | git subprocesses, smart-HTTP rig | skipped |
| 5013 | link | TestAHungRoomDoesNotStallThePrune | link, hub and git rig on loopback | skipped |
| 4990 | gitsync | TestAHubStoppedMidFetchLeavesEveryRefWhole | git subprocesses, smart-HTTP rig | skipped |
| 4827 | hubstore | TestOldSnapshotsThinOutRatherThanPilingUp | snapshots on disk | skipped |
| 4820 | gitsync | TestABranchOnlyTheForgeHasIsFoundAndFetchableThroughTheHub | git subprocesses, smart-HTTP rig | skipped |
| 4780 | gitsync | TestAPushOfMainFromACardIsRefusedAndFromTheOperatorLands | git subprocesses, smart-HTTP rig | skipped |
| 4635 | link | TestGitSyncAndCollectEndToEndOverTheLink | link, hub and git rig on loopback | skipped |
| 4610 | link | TestARoomsAnswerIsHeldBrieflyAndNotForever | link, hub and git rig on loopback | skipped |
| 4360 | gitsync | TestAGitFailureIsAnErrorAndNotANo | git subprocesses, smart-HTTP rig | skipped |
| 4340 | link | TestABranchPushedByARoomThatIsGoneNamesTheRoomAsAway | link, hub and git rig on loopback | skipped |
| 4340 | gitsync | TestPushedSaysMatchesBehindAheadDivergedAndNotPushed | git subprocesses, smart-HTTP rig | skipped |
| 4280 | gitsync | TestDeleteTagNotesAndStashFromACardAreRefusedAndNothingMoves | git subprocesses, smart-HTTP rig | skipped |
| 4250 | gitsync | TestAPushedBranchOfTheSameNameIsNotShadowed | git subprocesses, smart-HTTP rig | skipped |
| 4220 | link | TestNothingIsSentWhenNothingChanged | link, hub and git rig on loopback | skipped |
| 4220 | gitsync | TestTheOwnersSuccessorByChainPushesAndTakesTheBranchOver | git subprocesses, smart-HTTP rig | skipped |
| 4190 | link | TestAMissAnswersNotFoundWithTheClosestAndAsksNoRoom | link, hub and git rig on loopback | skipped |
| 4130 | gitsync | TestBranchSHAIsTheHubsTipOrNotFound | git subprocesses, smart-HTTP rig | skipped |
| 4110 | daemon | TestAClearMarkSurvivesAReadoptOfAPtyHostTerminal | ptys, processes, timers | skipped |
| 4110 | link | TestOnlyTheReachesOfAFetchMayAskForAURL | link, hub and git rig on loopback | skipped |
| 4069 | link | TestTheMoveIsRefusedWhileEitherRoomIsOffline | link, hub and git rig on loopback | skipped |
| 4030 | gitsync | TestAFirstAskMakesTheRepositoryFromItsForge | git subprocesses, smart-HTTP rig | skipped |
| 4020 | gitsync | TestAnotherCardCannotPushToAnOwnedBranchOnAnyRoom | git subprocesses, smart-HTTP rig | skipped |
| 4012 | link | TestARoomDoesNotAnnounceAtAHubThatKeepsNothing | link, hub and git rig on loopback | skipped |
| 4006 | link | TestAnAnnouncementThatFailedIsSentAgain | link, hub and git rig on loopback | skipped |
| 3990 | gitsync | TestReachableIsTheTipOrWhatItIsBuiltOn | git subprocesses, smart-HTTP rig | skipped |
| 3980 | gitsync | TestAPushTheLogCannotSettleStaysOwnedAndIsSettledByTheNextPush | git subprocesses, smart-HTTP rig | skipped |
| 3970 | link | TestABranchOnBothAnswersBothAndSaysWhetherTheRoomIsAhead | link, hub and git rig on loopback | skipped |
| 3960 | gitsync | TestMainIsRefreshedAtMostOncePerFreshFor | git subprocesses, smart-HTTP rig | skipped |
| 3920 | link | TestOneRequestCarriesThePushedReadForItsSource | link, hub and git rig on loopback | skipped |
| 3910 | api | TestBoardScriptParses | server, worktrees, database | skipped |
| 3896 | daemon | TestRestartRunnerLeavesAStubbornRunnerUp | ptys, processes, timers | skipped |
| 3890 | gitsync | TestPushedCarriesTheOwnerAndTheTimeOfTheLastPush | git subprocesses, smart-HTTP rig | skipped |
| 3780 | gitsync | TestDeleteAndTagRefusalsAreTheHubsSentences | git subprocesses, smart-HTTP rig | skipped |
| 3750 | link | TestACardMakesOneForItsOwnRoomsBranchAndTheHubReadsTheTip | link, hub and git rig on loopback | skipped |
| 3750 | gitsync | TestAMultiRefPushWithOneBadRefMovesNothing | git subprocesses, smart-HTTP rig | skipped |
| 3730 | gitsync | TestACrashBetweenGitAndTheLogLeavesTheBranchOwnedUntilStartupSettlesIt | git subprocesses, smart-HTTP rig | skipped |
| 3697 | link | TestAKeystrokeCrossesTheLinkWithoutDelay | link, hub and git rig on loopback | skipped |
| 3670 | link | TestACardWhoseRoomDoesNotSayWhereItsForwarderIsIsGivenNoURL | link, hub and git rig on loopback | skipped |
| 3650 | gitsync | TestPushedRefusesWhatIsNotANameOrACommitId | git subprocesses, smart-HTTP rig | skipped |
| 3637 | ptyhost | TestIdleExit | pty children, idle timers | skipped |
| 3630 | gitsync | TestTheOperatorMayFastForwardAnyBranchAndOwnershipIsUnchanged | git subprocesses, smart-HTTP rig | skipped |
| 3565 | daemon | TestAStartWritesItsRunRowBeforeTheLaunchIsComplete | ptys, processes, timers | skipped |
| 3560 | link | TestStashNotesAndAnUnservedBranchAreRefusedThroughTheHub | link, hub and git rig on loopback | skipped |
| 3560 | gitsync | TestATakeoverGitRefusesReleasesNothing | git subprocesses, smart-HTTP rig | skipped |
| 3540 | gitsync | TestAChainNamingAnotherRoomsCardDoesNotHelp | git subprocesses, smart-HTTP rig | skipped |
| 3530 | gitsync | TestPushedOfARepositoryTheHubLacksIsNotPushed | git subprocesses, smart-HTTP rig | skipped |
| 3520 | gitsync | TestAnAdoptedMirrorTakesWorkBranchesAndNothingThePassOwns | git subprocesses, smart-HTTP rig | skipped |
| 3520 | link | TestAHostThatIsNotANameAndAPortIsNotMadeIntoAURL | link, hub and git rig on loopback | skipped |
| 3490 | link | TestACardIsGivenNoURLForARoomsWorkInProgress | link, hub and git rig on loopback | skipped |
| 3490 | store | TestCompactRefusals | sqlite on disk | skipped |
| 3480 | gitsync | TestPushedCancelledIsAnErrorNotAState | git subprocesses, smart-HTTP rig | skipped |
| 3480 | gitsync | TestTheOwnerFastForwardsItsOwnBranch | git subprocesses, smart-HTTP rig | skipped |
| 3450 | gitsync | TestARoomAndTheOperatorCanFetchTheStore | git subprocesses, smart-HTTP rig | skipped |
| 3420 | link | TestABranchTheRoomWouldNotServeIsNeverListed | link, hub and git rig on loopback | skipped |
| 3390 | link | TestTheListFiltersByStateRoomTargetAndRepo | link, hub and git rig on loopback | skipped |
| 3360 | link | TestTheURLIsOnTheHostTheCallerReachedTheHubBy | link, hub and git rig on loopback | skipped |
| 3330 | gitsync | TestANonFastForwardIsRefusedAndMovesNothing | git subprocesses, smart-HTTP rig | skipped |
| 3300 | gitsync | TestReftableOnlyWhereGitHasItAndTheProbeWorks | git subprocesses, smart-HTTP rig | skipped |
| 3264 | daemon | TestARunnerAndAShellOnOneCardHaveTheirOwnRuns | ptys, processes, timers | skipped |
| 3223 | gitsync | TestItemFactsSeeThroughACherryPick | git subprocesses, smart-HTTP rig | skipped |
| 3215 | link | TestTheScopedStreamWithoutTheParameterIsUnchanged | link, hub and git rig on loopback | skipped |
| 3210 | gitsync | TestSyncInitThenOKThenResigned | git subprocesses, smart-HTTP rig | skipped |
| 3190 | gitsync | TestStashNotesAnUnservedBranchAndClaudeMainAreRefusedToAFetch | git subprocesses, smart-HTTP rig | skipped |
| 3170 | gitsync | TestSyncBehindWhenHubMainIsCheckedOutDirty | git subprocesses, smart-HTTP rig | skipped |
| 3140 | gitsync | TestABranchTheForgeDroppedIsDroppedFromTheStore | git subprocesses, smart-HTTP rig | skipped |
| 3050 | api | TestPRWorktreeRaisesAndClearsTheForgeAlert | server, worktrees, database | skipped |
| 3050 | link | TestACardOnAnotherRoomIsGivenItsRoomsForwarderAndFetchesThroughIt | link, hub and git rig on loopback | skipped |
| 3042 | link | TestPlacementDoesNotWaitForASlowRoom | link, hub and git rig on loopback | skipped |
| 3039 | link | TestABuildThatDoesNotMatchItsHashIsNotInstalled | link, hub and git rig on loopback | skipped |
| 3021 | daemon | TestOpencodeExecIsBoundedAgainstAChildHoldingStdout | ptys, processes, timers | skipped |
| 3015 | link | TestAHungRoomDoesNotStallThePinOrder | link, hub and git rig on loopback | skipped |
| 2950 | gitsync | TestAForgeThatCannotBeReachedLeavesMainAndTheFetchGoesOn | git subprocesses, smart-HTTP rig | skipped |
| 2950 | gitsync | TestACaseVariantOfAnExistingBranchIsRefused | git subprocesses, smart-HTTP rig | skipped |
| 2938 | link | TestAutoApprovesFromTheEventWithNoBoardAndNoPolling | link, hub and git rig on loopback | skipped |
| 2930 | gitsync | TestTheListingTakesOwnersAndTimesFromThePushLog | git subprocesses, smart-HTTP rig | skipped |
| 2900 | gitsync | TestTwoPushesToTheSameNewBranchHaveExactlyOneWinner | git subprocesses, smart-HTTP rig | skipped |
| 2887 | daemon | TestScreenAgainstXterm | ptys, processes, timers | skipped |
| 2883 | runnersetup | TestConcurrentClaudeWritesKeepEveryChange | file locking | skipped |
| 2860 | gitsync | TestTheNextPushToARepositorySettlesWhatACrashLeftPending | git subprocesses, smart-HTTP rig | skipped |
| 2840 | gitsync | TestCollectBringsNewBranchesPrunesDeletedAndNeverCollectsClaudeMain | git subprocesses, smart-HTTP rig | skipped |
| 2840 | gitsync | TestTheRoomServesClaudeBranchesExceptMainAndNothingElse | git subprocesses, smart-HTTP rig | skipped |
| 2840 | gitsync | TestAHostileRefNameSet | git subprocesses, smart-HTTP rig | skipped |
| 2805 | daemon | TestChangesTurnWithHundredsOfPathsFitsTheCommandLine | ptys, processes, timers | skipped |
| 2780 | gitsync | TestTheStoreAndTheGitReposMirrorsCoexist | git subprocesses, smart-HTTP rig | skipped |
| 2770 | gitsync | TestAPushTheLogCannotBeginMovesNothing | git subprocesses, smart-HTTP rig | skipped |
| 2760 | gitsync | TestAWrongRevokedOrOtherCardsTokenIsRefused | git subprocesses, smart-HTTP rig | skipped |
| 2740 | link | TestABranchOnAnOnlineRoomAloneAnswersTheRoomsURL | link, hub and git rig on loopback | skipped |
| 2730 | deployready | TestNewestVerdictWins | git subprocesses | skipped |
| 2720 | api | TestListTasksLauncherCostWithManyCards | server, worktrees, database | skipped |
| 2690 | link | TestAChunkedPostGoesThroughForwarderLinkAndCgi | link, hub and git rig on loopback | skipped |
| 2680 | gitsync | TestARepositorysOwnHookAndConfigAreNotUsed | git subprocesses, smart-HTTP rig | kept |
| 2620 | link | TestAShallowOrFilteredFetchIsRefusedBeforeItIsPassedOn | link, hub and git rig on loopback | skipped |
| 2620 | gitsync | TestAPushAsksOnlyAFewOwnersAndTheRestStayOwned | git subprocesses, smart-HTTP rig | skipped |
| 2600 | gitsync | TestSyncBehindWhenAWorktreeHoldsClaudeMain | git subprocesses, smart-HTTP rig | skipped |
| 2590 | link | TestAnOfflineRoomAnswersOfflineBeforeAnyoneFetches | link, hub and git rig on loopback | skipped |
| 2570 | gitsync | TestAnInitOfAMirrorTakesTheMirrorsLock | git subprocesses, smart-HTTP rig | skipped |
| 2560 | gitsync | TestAnUnreachableOwnersRoomKeepsTheBranchOwnedAndSaysWhy | git subprocesses, smart-HTTP rig | skipped |
| 2544 | daemon | TestAFloodOfLoginsIsBounded | ptys, processes, timers | skipped |
| 2540 | store | TestBySourceExternalIsKeyedOnThePair | sqlite on disk | skipped |
| 2530 | api | TestPRWorktreeSameRepoUsesTheRealBranch | server, worktrees, database | skipped |
| 2520 | gitsync | TestViewIsTheExactShapeTheBoardReads | git subprocesses, smart-HTTP rig | skipped |
| 2509 | link | TestTypingHoldsTheCountdownBack | link, hub and git rig on loopback | skipped |
| 2500 | gitsync | TestTheOperatorReleasesABranchAndTheNextCardOwnsIt | git subprocesses, smart-HTTP rig | skipped |
| 2500 | gitsync | TestSyncAddsTheHubRemoteAndGuardsTheOriginOfACloneItMade | git subprocesses, smart-HTTP rig | skipped |
| 2490 | api | TestPRWorktreeForkUsesPRNumber | server, worktrees, database | skipped |
| 2490 | store | TestColdSinkFailureNeverFailsAppend | sqlite on disk | skipped |
| 2445 | daemon | TestNoAckNeverClears | ptys, processes, timers | skipped |
| 2430 | store | TestAMovedCardKeepsItsHandle | sqlite on disk | skipped |
| 2430 | api | TestPRWorktreeSecondCallIsTheAnswer | server, worktrees, database | skipped |
| 2430 | link | TestOnlyTheRecordedMergerMovesClaudeMain | link, hub and git rig on loopback | skipped |
| 2420 | store | TestCompactCopiesEverythingAndSwitchesMode | sqlite on disk | skipped |
| 2420 | store | TestABadLinkNeverReachesACard | sqlite on disk | skipped |
| 2420 | deployready | TestSecondPassReadsAlmostNothingAndAgrees | git subprocesses | skipped |
| 2420 | deployready | TestRoomSideNeedsARoomVerdict | git subprocesses | skipped |
| 2390 | gitsync | TestGitPushNoneRefusesReceivePackAndStillFetches | git subprocesses, smart-HTTP rig | skipped |
| 2390 | store | TestUnkeyedPermissionsDoNotCollide | sqlite on disk | skipped |
| 2390 | deployready | TestHoldThenHubOkOnlyStillHoldsTheRoom | git subprocesses | skipped |
| 2381 | daemon | TestIdleParkRule | ptys, processes, timers | skipped |
| 2380 | gitsync | TestAnOldShaThatIsNotTheRefsCurrentValueIsRefused | git subprocesses, smart-HTTP rig | skipped |
| 2380 | store | TestDeleteProviderCascadesToReposAndLeavesCardsAlone | sqlite on disk | skipped |
| 2380 | store | TestAnswerDraftsRoundTripOnTheCard | sqlite on disk | skipped |
| 2340 | api | TestFindingsNeedAReadyRow | server, worktrees, database | skipped |
| 2340 | link | TestTheHubForgeEntries | link, hub and git rig on loopback | skipped |
| 2340 | store | TestACardRemembersItsLaunchOptions | sqlite on disk | skipped |
| 2320 | gitsync | TestAMissingBranchIsStillNotFound | git subprocesses, smart-HTTP rig | skipped |
| 2320 | gitsync | TestACardsPushOfANewBranchLandsAndIsLogged | git subprocesses, smart-HTTP rig | skipped |
| 2316 | link | TestAnEventPastTheDeadlineIsNotApproved | link, hub and git rig on loopback | skipped |
| 2310 | api | TestAbortRetryAndStartGuards | server, worktrees, database | skipped |
| 2310 | gitsync | TestAClientsOwnCardHeadersNeverReachTheHub | git subprocesses, smart-HTTP rig | skipped |
| 2290 | api | TestAMissingRowIsThePullsNotFound | server, worktrees, database | skipped |
| 2290 | deployready | TestRangeIsFirstParentOnly | git subprocesses | skipped |
| 2290 | store | TestMovingBetweenFinishedStatusesStaysArchived | sqlite on disk | skipped |
| 2280 | link | TestReposIsOpenLikeGrowlsAndExactlyShaped | link, hub and git rig on loopback | skipped |
| 2280 | store | TestThePtyRunMigrationAppliesFreshAndToAnExistingDatabase | sqlite on disk | skipped |
| 2250 | store | TestSoundIsStoredOnTheCard | sqlite on disk | skipped |
| 2250 | gitsync | TestAHandMadeCloneWithNoHubRemoteIsPushedByTheURLItsPlaceNames | git subprocesses, smart-HTTP rig | skipped |
| 2240 | store | TestClaimKeepsTheSuggestedDirectoryWhenTheRunnerHasNone | sqlite on disk | skipped |
| 2240 | gitsync | TestSyncClearsAStaleOwnedLockBeforeFetching | git subprocesses, smart-HTTP rig | skipped |
| 2230 | store | TestGlobalAutoWithADeadline | sqlite on disk | skipped |
| 2220 | link | TestTheHubForgeCheckAnswersAndRaisesNothing | link, hub and git rig on loopback | skipped |
| 2210 | store | TestWithdrawOfferedOnlyTakesAnInboxCard | sqlite on disk | skipped |
| 2210 | store | TestShareSurvivesAShutdownAndDiesOnAStop | sqlite on disk | kept |
| 2200 | store | TestAPidlessReportKeepsThePidOnlyForTheSameSession | sqlite on disk | skipped |
| 2190 | store | TestALastSizeRoundTripsBothSides | sqlite on disk | skipped |
| 2190 | gitsync | TestAPushThatMakesGitProbeLandsThroughTheReceiver | git subprocesses, smart-HTTP rig | skipped |
| 2180 | link | TestAFetchOfAClaudeBranchPassesThroughAndTheHubStoresNothing | link, hub and git rig on loopback | skipped |
| 2172 | daemon | TestCullExitsASupervisedWorkerFirst | ptys, processes, timers | skipped |
| 2150 | store | TestALiveCardBeatsADoneOneForAnAlias | sqlite on disk | skipped |
| 2140 | store | TestEventSinkSettingAddsFileColdSink | sqlite on disk | skipped |
| 2130 | store | TestUnpinningKeepsThePlace | sqlite on disk | skipped |
| 2130 | gitsync | TestAHubRemotePointingElsewhereIsLeftAloneAndAtriumHubIsAdded | git subprocesses, smart-HTTP rig | skipped |
| 2130 | store | TestSessionUsageKeepsHistory | sqlite on disk | skipped |
| 2130 | store | TestTheDefaultBackfillRunsOnce | sqlite on disk | skipped |
| 2130 | store | TestPRStatesStampAndClear | sqlite on disk | skipped |
| 2126 | daemon | TestAnUntaggedPeerCalledOrchestratorGetsNothingAndIsNotRemembered | ptys, processes, timers | skipped |
| 2110 | link | TestAnAuthFailureFetchingTheHeadRaisesTheAlert | link, hub and git rig on loopback | skipped |
| 2110 | store | TestAnEmptyWorktreeIsFilledOnceThenNeverMoves | sqlite on disk | skipped |
| 2110 | gitsync | TestAClonesDefaultBranchWhateverItIsCalledIsNeverServedToALiveCard | git subprocesses, smart-HTTP rig | skipped |
| 2100 | store | TestAMarkIsDueOnlyOnceItsTimeHasCome | sqlite on disk | skipped |
| 2100 | store | TestADeadlineRoundTripsThroughTheDatabase | sqlite on disk | skipped |
| 2100 | gitsync | TestPushToHubPushesAPlainBranchThroughTheForwarder | git subprocesses, smart-HTTP rig | skipped |
| 2093 | gitsync | TestTwoCaseTwinRepositoriesMadeAtOnceLeaveOne | git subprocesses, smart-HTTP rig | skipped |
| 2090 | deployready | TestSignatureMovesWithTheAnswer | git subprocesses | skipped |
| 2090 | link | TestARoomPushesToTheHubStoreOverTheLinkAsItselfWithItsCard | link, hub and git rig on loopback | skipped |
| 2090 | store | TestTheMigrationCarriesAnAskFromTheOldColumns | sqlite on disk | skipped |
| 2080 | link | TestABranchOnTheHubAloneAnswersTheHubsURL | link, hub and git rig on loopback | skipped |
| 2070 | store | TestMigrationsAreIdempotent | sqlite on disk | skipped |
| 2070 | store | TestArchiveTakesDeadCardsOffTheBoard | sqlite on disk | skipped |
| 2055 | daemon | TestThePTYFollowsTheWindowThatWasLastUsedAndReturnsOnDetach | ptys, processes, timers | skipped |
| 2050 | store | TestLaunchOptionsMigrationBackfillsOnlyEmptyRows | sqlite on disk | skipped |
| 2049 | link | TestARoomDoesNotInstallTheBinaryItIsAlreadyRunning | link, hub and git rig on loopback | skipped |
| 2043 | link | TestAHubCannotUpgradeARoomThatDidNotAgree | link, hub and git rig on loopback | skipped |
| 2040 | link | TestAnotherPlatformsBuildIsRefused | link, hub and git rig on loopback | skipped |
| 2040 | gitsync | TestARepeatedCardHeaderIsRefused | git subprocesses, smart-HTTP rig | skipped |
| 2040 | store | TestDeletingAThemeLeavesTheOthers | sqlite on disk | skipped |
| 2040 | store | TestOrgsAreDerivedFromRows | sqlite on disk | skipped |
| 2036 | link | TestManualMoveUpdatesTheClaim | link, hub and git rig on loopback | skipped |
| 2030 | link | TestReadersWithNoAddressShareTheReachsBudgetAndPeersWithOneDoNot | link, hub and git rig on loopback | skipped |
| 2030 | deployready | TestGitTooOldForRemergeDiffCountsMergesAsCarryingNothing | git subprocesses | skipped |
| 2030 | api | TestPRWorktreeWithNoCheckoutGoesThroughTheClonePath | server, worktrees, database | skipped |
| 2028 | guard | TestGitGuard | git subprocess | skipped |
| 2025 | daemon | TestAWokenCardThatNeverTakesItsMessagesTellsTheLauncher | ptys, processes, timers | skipped |
| 2021 | link | TestOfflineOwnerWarnsAndDoesNotMove | link, hub and git rig on loopback | skipped |
| 2020 | gitsync | TestARefusalReachesGitAsRemoteRejectedWithTheSentence | git subprocesses, smart-HTTP rig | skipped |
| 2020 | deployready | TestMergeNeedsAVerdictOnlyForItsResolution | git subprocesses | skipped |
| 2011 | daemon | TestHeldMarkIsForcedOutByTime | ptys, processes, timers | skipped |
| 2010 | link | TestARequestIntoMainRaisesAQuestionForTheOperatorAndItEndsWithTheRequest | link, hub and git rig on loopback | skipped |
| 2003 | gitsync | TestARepositoryWithoutTheCommitIsSkippedAndNotSearched | git subprocesses, smart-HTTP rig | skipped |
