# Timing for one merged non-flaky test pool (stage 1)

Read-only report for backlog item `q-pool-timing-1`. Nothing in the workflow, `scripts/ci.sh` or any test was
changed. The aim is the numbers needed to split every non-flaky test, Go and board, into CI jobs of equal time.

Source: ci run 40193874441 on main, commit b085d37, all green, every regular job run once. The flaky suite was empty,
so the six flaky jobs were skipped and every test that ran is in the non-flaky pool. Every number was read from that
run's artefacts through the hub (`steps.tsv`, `summary.md`, `go-test.report.txt`, `board.log`) or computed from the
job start and end times in the run record. A number marked INFERRED is an estimate, not a measurement.

Limit of the source: the hub reads an artefact file only from its end, at most 64 KiB. `timings.sorted.tsv` holds
every Go test (6779 rows in windows go-test-2) slowest first, so only the slowest tests (from the reports) and the
fastest are readable this way. The full per-test Go tables are in each go job's artefact as `timings.sorted.tsv`.

## 1. Jobs as they run today

Wall is the job's start to end in the run record. Steps are seconds from `steps.tsv`. Outside is wall minus the
steps: checkout, setup-go, setup-node, upload. A count in brackets is tests run, or units for a board step.

| os | job | wall | test steps (s) | other steps (s) | outside |
| --- | --- | --- | --- | --- | --- |
| windows | go-test-2 | 987 | go-test 914 (6825 tests) | machine 8, time-guard 17 | 48 |
| windows | go-test-1 | 586 | go-test 518 (2410 tests) | machine 7, time-guard 17 | 44 |
| windows | build-board-1 | 471 | board 230 (201 units) | machine 7, toolchain 24, go-build 164 | 46 |
| windows | go-checks-visual | 390 | board-visual 89 (36), ps-tests 78 (889) | go-vet 114, vulncheck 28, rest 35 | 46 |
| windows | board-2 | 356 | board 265 (198 units) | machine 35, toolchain 27 | 29 |
| macos | go-test | 481 | go-test 301 (5746 tests) | go-build 128, time-guard 13 | 39 |
| macos | board-1 | 348 | board 317 (201 units) | toolchain 10 | 21 |
| macos | board-2 | 341 | board 312 (198 units) | toolchain 14 | 15 |
| macos | go-checks-visual | 232 | go-race 51 (650), board-visual 76 (36) | toolchain 13, go-vet 47, vulncheck 16 | 29 |
| ubuntu | go-all | 474 | go-test 245 (5740) | go-vet 72, go-build 21, vulncheck 21, time-guard 10, rest 2 | 103 |
| ubuntu | race-board-2 | 353 | go-race 87 (646 tests), board 178 (198 units) | toolchain 2 | 86 |
| ubuntu | board-1-visual | 341 | board 222 (201 units), board-visual 72 (36 units) | toolchain 3 | 44 |

The run took 988 s from its first job start to the end of go-test-2 (12:36:58 to 12:53:26). Every other job had
ended by 12:46:44, so windows go-test-2 alone held the run for its last 402 s.

## 2. Windows go-test-2 runs most of the Go suite twice

go-test-1 ran 2410 tests and go-test-2 ran 6825. The whole suite is 5740 tests on ubuntu and 5746 on macos, so the two
windows halves together ran about 3500 tests twice. go-test-2's report lists the part 1 packages a second time
(gitsync 441.1 s, api 150.1 s, cli 49.0 s, deployready 23.0 s, ptyhost 18.3 s, roomspec 6.5 s) and internal/daemon
twice: 211.8 s for its own `-skip '^Test[A-F]'` binary and 522.6 s for the whole of daemon run again inside the rest.

Cause, reproduced locally on claude/main with the pipeline of `scripts/ci.sh` lines 597 to 598: the repository is a
Go workspace, so `mod="$(go list -m)"` is every module path on its own line, not one path. The exclusion list
`echo "$mod/${p#./}"` then holds the module paths plus one bad line per package, and `grep -v -x -F` leaves
`internal/api`, `internal/cli` and `internal/daemon` in `rest`. Only windows runs the go-test parts, so only windows
pays. A fix is not part of this item.

Go package seconds on windows with the duplicates taken out (a package's time is its test binary's wall time):

| part | package | seconds |
| --- | --- | --- |
| 1 | internal/gitsync | 416.6 |
| 1 | internal/daemon, `^Test[A-F]` | 278.6 |
| 1 | internal/api | 153.5 |
| 1 | internal/cli | 54.3 |
| 1 | internal/deployready | 20.6 |
| 1 | internal/ptyhost | 20.4 |
| 1 | internal/roomspec | 12.9 |
| 2 | internal/link | 331.2 |
| 2 | internal/daemon, the rest | 211.8 |
| 2 | internal/hubstore | 186.3 |
| 2 | internal/store | 118.6 |
| 2 | internal/worktrees | 15.5 |
| 2 | internal/cardreview | 11.3 |
| 2 | internal/roomstats | 5.0 |
| 2 | internal/base/detach | 4.7 |
| 2 | internal/guard | 4.1 |
| 2 | internal/runnersetup | 2.3 |
| 2 | 37 more packages | 4.1 |

Part 1 is 956.9 package seconds and its step took 518 s. Part 2 without the duplicates is 894.9 package seconds:
daemon's 211.8 s beside 683.1 s of the rest at `-p 3`, whose longest package is link at 331.2 s. INFERRED: a correct
exclusion puts go-test-2's step near 350 s and the job near 420 s, about 9.5 minutes off the run.

## 3. Go buckets and tests

Summed test time per bucket on windows, from each part's report. A bucket's sum counts parallel tests in full, so it
can exceed its package's wall time. go-test-2's buckets include the duplicated packages of section 2.

| bucket | go-test-1 tests | go-test-1 s | go-test-2 tests | go-test-2 s |
| --- | --- | --- | --- | --- |
| agent | 1062 | 540.7 | 2912 | 1304.5 |
| gitsync | 288 | 949.1 | 288 | 1053.9 |
| store | | | 470 | 290.4 |
| hub/git | | | 53 | 194.7 |
| hubstore | | | 225 | 186.2 |
| api/files | 84 | 123.7 | 84 | 107.4 |
| hub/pulls | | | 73 | 118.4 |
| hub/deploy | | | 59 | 73.8 |
| api/rest | 60 | 61.7 | 60 | 63.0 |
| hub/merge | | | 10 | 62.8 |
| cli | 322 | 54.1 | 322 | 48.7 |
| hub/rooms | | | 251 | 47.8 |
| api/cards | 31 | 43.9 | 31 | 37.2 |
| api/close | 17 | 42.4 | 17 | 41.5 |
| api/prs | 67 | 38.9 | 67 | 35.2 |
| hub/cihook | | | 62 | 35.4 |
| api/providers | 26 | 24.4 | 26 | 31.7 |
| api/launch | 17 | 23.1 | 17 | 24.1 |
| ptyhost | 26 | 20.2 | 26 | 18.2 |
| api/skins | 15 | 19.7 | 15 | 20.6 |
| hub/files | | | 26 | 18.6 |
| hub/notify | | | 86 | 17.2 |
| worktrees | | | 12 | 15.4 |
| api/settings | 24 | 13.0 | 24 | 12.5 |
| api/termopen | 11 | 7.7 | 11 | 13.4 |
| hub/items | | | 122 | 12.7 |
| hub/rest | | | 94 | 10.8 |
| api/sessions | 10 | 10.3 | 10 | 8.4 |
| mcp/docs | | | 12 | 10.1 |
| mcp/files | | | 12 | 9.7 |
| api/usage | 15 | 7.1 | 15 | 9.3 |
| hub/backlog | | | 15 | 8.2 |
| the other buckets | 12 | 4.9 | 249 | 15.1 |

Slowest Go tests, seconds, per os. Windows lists both parts, ubuntu and macos run the whole suite in one job.

| os | s | package | test |
| --- | --- | --- | --- |
| windows 1 | 22.46 | gitsync | TestDeleteTagNotesAndStashFromACardAreRefusedAndNothingMoves |
| windows 1 | 19.74 | gitsync | TestEpochMoveKeepsWhatLandedAfterClintFetched |
| windows 1 | 19.41 | gitsync | TestMergeRunnerTestsTheCandidateAndNamesWhatFailed |
| windows 1 | 18.05 | gitsync | TestAChunkedPostGoesThrough |
| windows 1 | 16.61 | gitsync | TestBranchStatusCallsLandedByContentNotAncestry |
| windows 1 | 16.41 | gitsync | TestAMultiRefPushWithOneBadRefMovesNothing |
| windows 1 | 16.26 | gitsync | TestAPushOfMainFromACardIsRefusedAndFromTheOperatorLands |
| windows 1 | 15.51 | gitsync | TestTheOwnerFastForwardsItsOwnBranch |
| windows 1 | 13.62 | gitsync | TestANonFastForwardIsRefusedAndMovesNothing |
| windows 1 | 13.06 | gitsync | TestAHostileRefNameSet |
| windows 1 | 10.23 | ptyhost | TestStartRunsADetachedHostFromACopy |
| windows 2 | 20.82 | store | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| windows 2 | 20.40 | link | TestAChunkedPostGoesThroughForwarderLinkAndCgi |
| windows 2 | 17.22 | hubstore | TestOldSnapshotsThinOutRatherThanPilingUp |
| windows 2 | 16.14 | hubstore | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| windows 2 | 15.16 | store | TestOpeningTheSameStoreTogetherNeverFailsMidMigration |
| ubuntu | 8.90 | store | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| ubuntu | 8.81 | ptyhost | TestStartRunsADetachedHostFromACopy |
| ubuntu | 8.32 | daemon | TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated |
| ubuntu | 8.01 | cli | TestEveryHookFailsOpenInsideItsBudget |
| ubuntu | 6.35 | daemon | TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize |
| ubuntu | 6.01 | link | TestAnAnswerWithNoCardListIsNotAnEmptyRoom |
| ubuntu | 6.01 | link | TestARoomInTroubleDoesNotAnnounceItselfEmpty |
| ubuntu | 5.13 | store | TestOpeningTheSameStoreTogetherNeverFailsMidMigration |
| ubuntu | 5.01 | link | TestAHungRoomDoesNotStallThePrune |
| ubuntu | 4.41 | hubstore | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| macos | 15.57 | store | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| macos | 8.84 | ptyhost | TestStartRunsADetachedHostFromACopy |
| macos | 8.47 | daemon | TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated |
| macos | 8.02 | cli | TestEveryHookFailsOpenInsideItsBudget |
| macos | 8.01 | gitsync | TestAChunkedPostGoesThrough |
| macos | 6.99 | link | TestAChunkedPostGoesThroughForwarderLinkAndCgi |
| macos | 6.58 | link | TestMergeAConflictWhoseMergerStoppedIsRefused |
| macos | 6.50 | link | TestMergeLandPutsOneCommitOnMainAndLeavesClaudeMain |
| macos | 6.48 | daemon | TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize |
| macos | 6.02 | link | TestARoomInTroubleDoesNotAnnounceItselfEmpty |

The windows gitsync tests are at least twice their macos times: TestAChunkedPostGoesThrough is 18.05 s against 8.01 s,
and no other gitsync test is in the ubuntu or macos top ten. gitsync is windows' longest single
package (416.6 s). It is the floor for any windows job that holds gitsync whole.

## 4. Board units

| os | half | units | sum of units | shards | wall | step |
| --- | --- | --- | --- | --- | --- | --- |
| windows | board-1 | 201 | 734 s | 4 | 210 s | 230 s |
| windows | board-2 | 198 | 917 s | 4 | 238 s | 265 s |
| macos | board-1 | 201 | 785 s | 3 | 309 s | 317 s |
| macos | board-2 | 198 | 889 s | 3 | 302 s | 312 s |
| ubuntu | board-1 | 201 | 748 s | 4 | 216 s | 222 s |
| ubuntu | board-2 | 198 | 654 s | 4 | 174 s | 178 s |
| each os | board-visual | 36 | | | | 72 to 89 s, non-gating |

On 4 shards the board runs at 3.5 to 3.9 unit seconds per wall second, on macos' 3 shards 2.5 to 2.9. A half's
`board` step is its wall plus 4 to 27 s of start and checks.

The windows board-2 half is 183 s heavier than board-1 by unit sum. Of 393 distinct units, 38 take 10 s or more
(571 s together), 71 take 5 to 10 s, 124 take 2 to 5 s, 102 take 1 to 2 s and 58 under 1 s.

Four names appear more than once in `board.log`: `trayHead` twice in each half, `emptyGroups`, `noResumeButton` and
`trayToast` twice in one half. The log does not say whether that is one unit run twice or two units sharing a name.

Every unit on windows, seconds, slowest first (both halves, one entry per name):

```
termProbeStorm            27.3    mOlder                    27.2    peekEverywhere            26.6
bootClean                 25.6    atriumDown                25.3    mFollow                   24.2
peekOneHover              21.3    groupColor                19.7    core2                     17.8
growlActions              17.8    u016                      17.7    mReload                   15.4
gauto                     14.8    cardUrlWayOut             14.6    pasteBig                  14.6
termLag                   14.4    cacheChip                 13.3    burnReadout               13.2
growlPhone                12.8    cardUrlTable              12.1    eventDriven               12.1
land                      12.0    mCard                     11.8    growlOnIt                 11.7
mChanges                  11.3    hubReposFeed              11.2    busyGuard                 11.0
skinScope                 11.0    usageLimits               10.6    workerCardColors          10.6
mBubbles                  10.5    questionsClick            10.4    controlsCompact           10.3
termListLastRow           10.3    hubReposShelf             10.2    mPerms                    10.2
newCard                   10.0    repliesOf                 10.0    quietDoer                  9.8
mHomeLive                  9.5    hubReposLedger             9.4    mReconnect                 9.4
pullsPage                  9.3    coverPoll                  9.0    mStickBottom               9.0
growlPopout                8.8    restartGate                8.7    untaggedSort               8.7
pulls                      8.4    pullsDrawer                8.4    mTypeSteady                8.3
newWorkDialog              8.3    tallPty                    8.3    popoutRaise                8.1
usageGroups                8.0    chatterTab                 7.9    mHomeOrder                 7.9
deepLinkRequests           7.8    usagePolish                7.8    growlOff                   7.6
termMenu                   7.6    mReview                    7.4    readyPopout                7.4
hubTransports              7.3    groupMenus                 7.2    mViewer                    7.2
foldStill                  6.9    growlChoiceOnce            6.9    idleRate                   6.9
termDebug                  6.9    fileLinksView              6.8    quickPaste                 6.8
typing                     6.8    groupDrag                  6.7    hubRepos                   6.7
mCompose                   6.7    windowMute                 6.7    restartStays               6.6
sshBadge                   6.5    walkNoResize               6.4    growlStable                6.2
hubReposBranches           6.2    walkContext                6.2    ciGrowl                    6.1
laneFold                   6.1    mReadRetry                 6.1    detailsHover               6.0
peekStatusTruth            5.9    toastsTop                  5.8    composeImages              5.7
mRecapSheet                5.6    deepLinkView               5.5    pasteBusy                  5.5
pollsGone                  5.5    backlogSearch              5.4    changeReqStates            5.4
cleanCopy                  5.4    heldEnvelope               5.4    idleBudget                 5.4
keepAlive                  5.4    roomsDash                  5.4    mMarkdown                  5.2
blockerMark                5.1    cardUrlClash               5.1    contextSize                5.1
gearHosts                  5.1    mCardUpload                5.1    liveHome                   5.0
termsDrawerPush            5.0    boardUsage                 4.9    mNoSideways                4.9
mResume                    4.9    termDeadCard               4.9    itemsMock                  4.8
mHostile                   4.8    peekDrawers                4.8    mCardUrl                   4.7
replies                    4.7    mHome                      4.6    mOwnMessages               4.6
roomsBack                  4.5    cardUrlLinks               4.4    itemDocsLanded             4.4
terminalTopGap             4.4    themePreview               4.4    fileView                   4.3
mSendFree                  4.3    termEnded                  4.3    crossedPane                4.2
detailsSide                4.2    notifyOff                  4.2    termWear                   4.2
backlogShow                4.1    growlChoices               4.1    mAgentIdle                 4.1
skinHeal                   4.1    topNav                     4.1    carryLink                  4.0
hears                      4.0    launchLink                 4.0    mBell                      4.0
roomsMachine               4.0    growlReplyGrow             3.9    stuck                      3.9
termDebugPerCard           3.9    backlogPrios               3.8    boardDocs                  3.8
boardFiles                 3.8    mGroupMenus                3.8    mOpenQ                     3.8
popoutTagFlip              3.7    reselect                   3.7    toastLives                 3.7
deepLinkBack               3.6    growlQuestionBody          3.6    itemsView                  3.6
restartMenu                3.6    backlogPriority            3.4    clock                      3.4
mHidden                    3.4    cardViewBarInline          3.3    chatter                    3.3
contextCycle               3.3    dropdownsInstant           3.3    growlRemind                3.3
pinNow                     3.3    prefsEverywhere            3.3    itemsUx                    3.2
linkBoard                  3.2    popoutBell                 3.2    chatKeep                   3.1
childFold                  3.1    clearMark                  3.1    copySelect                 3.1
pasteDone                  3.1    pullsPasteOpening          3.1    borderColourLag            3.0
closeOneDialog             3.0    composePaste               3.0    growlLinks                 3.0
termLinkLate               3.0    attachAtOnce               2.9    core                       2.9
growlStack                 2.9    backForward                2.8    idlePark                   2.8
mActivityRead              2.8    settingsOnce               2.8    burnChart                  2.7
cardUrlWinName             2.7    findingsProbe              2.7    mCompact                   2.7
mGrowl                     2.7    usageWeekReset             2.7    mSwitcher                  2.6
mTables                    2.6    notifyCommand              2.6    termHiddenRedraw           2.6
alias                      2.5    greyMeansDead              2.5    growlModal                 2.5
itemMinionChip             2.5    mOutputAt                  2.5    termRowBleed               2.5
backlogAbsent              2.4    mPinch                     2.4    roomsMenuScroll            2.4
shiftMenu                  2.4    tabsOrder                  2.4    termMinionHidden           2.4
usageTab                   2.4    backlogGroup               2.3    changeReqUnseenBranch      2.3
clearLine                  2.3    linkReuse                  2.3    repoPaste                  2.3
termFitHeight              2.3    termSortStarted            2.3    themeLab                   2.3
chatPaste                  2.2    fileMenuHTML               2.2    looksIdle                  2.2
readyTwoWindows            2.2    usageCacheReads            2.2    backtickPathLinks          2.1
bridge                     2.1    growlTrayRow               2.1    arrivedOnce                2.0
backlogSort                2.0    cardRowChips               2.0    discreteGroup              2.0
mHomePicker                2.0    termListPatch              2.0    cardUrlRoom                1.9
changeReqOpen              1.9    chatBridge                 1.9    docsPage                   1.9
growlQuiet                 1.9    pasteOldRoom               1.9    prMove                     1.9
presence                   1.9    trayHead                   1.9    cardUrlNotify              1.8
changeReqMockOff           1.8    changeReqShareForbid       1.8    cleanCopyTwo               1.8
ctxLine                    1.8    heldLink                   1.8    itemsRealShape             1.8
soundPhone                 1.8    termBox                    1.8    termMinionsChip            1.8
keepalive                  1.7    launchThrowaway            1.7    pullsLiveSteps             1.7
readTabSize                1.7    switcherRank               1.7    docShare                   1.6
pasteEmpty                 1.6    pasteSpinner               1.6    pasteStart                 1.6
recogniserRepo             1.6    changeReqShaRule           1.5    changeReqStateWord         1.5
chatCap                    1.5    groupBridge                1.5    groupingPerView            1.5
history                    1.5    hubReposTimelineDots       1.5    loginNeeded                1.5
mChangesReal               1.5    mPictures                  1.5    mPrompts                   1.5
peekHoverRest              1.5    readyNoSeed                1.5    autoPerm                   1.4
changeReqClose             1.4    hiddenEveryLevel           1.4    itemsEfficacy              1.4
notifyTrayFill             1.4    switches                   1.4    termDragNotTyped           1.4
turnLimits                 1.4    backlogBar                 1.3    boardMinionHidden          1.3
changeReqOneRead           1.3    gearTermList               1.3    growlAttention             1.3
landThePlane               1.3    mPickerDone                1.3    mPrefsEverywhere           1.3
mailChip                   1.3    mergeStrip                 1.3    minionToggleShared         1.3
pasteClose                 1.3    readWidthSkin              1.3    readyOnce                  1.3
toastStays                 1.3    changeReqRecord            1.2    ctxLimitLayers             1.2
fileMenu                   1.2    mCleanUpForeign            1.2    pathBadges                 1.2
popupFit                   1.2    revealFolds                1.2    themePicker                1.2
transportChips             1.2    usageTurns                 1.2    detailsOne                 1.1
emptyGroups                1.1    hostLabelGroup             1.1    joinedClick                1.1
keepaliveMark              1.1    noReadyChildren            1.1    notifyTrayWrap             1.1
offCardActions             1.1    pullsAbsent                1.1    roomsSetup                 1.1
backlogRows                1.0    cardLabelAlias             1.0    changeReqBehind            1.0
changeReqClosedMerged      1.0    changeReqDiverged          1.0    changeReqMock              1.0
changeReqReadOnly          1.0    changeReqRefused           1.0    chatFast                   1.0
childUnderParent           1.0    coldHandoff                1.0    hubGate                    1.0
launchRepoUrls             1.0    minionNeedsYou             1.0    sayWhen                    1.0
stackMinionHidden          1.0    trayToast                  1.0    changeReqCanon             0.9
interviewTop               0.9    mComposeImages             0.9    nestCrossRoom              0.9
oneTooltip                 0.9    peekKids                   0.9    repoColors                 0.9
revealChip                 0.9    changeReqList              0.8    changeReqPhone             0.8
changeReqText              0.8    changeReqWithdraw          0.8    clearKeepsPage             0.8
homesLaunch                0.8    htmlTab                    0.8    itemOpenHiddenCard         0.8
itemsPhone                 0.8    mChatThumbs                0.8    minionReadyQuiet           0.8
noResumeButton             0.8    sayEnter                   0.8    termHeaderSize             0.8
transportToggle            0.8    zitiLogo                   0.8    alertKinds                 0.7
cardRoute                  0.7    changeReqLedgerRows        0.7    changeReqOwnForbid         0.7
joinedLive                 0.7    kidCounts                  0.7    launchSetup                0.7
pausedDead                 0.7    peekScratch                0.7    pullsPanelStage            0.7
unifiedCard                0.7    cycleChip                  0.6    homesChips                 0.6
mServe                     0.6    markdownTab                0.6    sendArrow                  0.6
stackPinnedSort            0.6    termActivityWorkingFirst   0.6    termRowChips               0.6
u002                       0.6    zrokLogo                   0.6    askAgain                   0.5
homesRoot                  0.5    mGrowlStack                0.5    newContextSse              0.5
peekLaunch                 0.5    homesDisk                  0.4    minionPredicate            0.4
parkedStays                0.4    keepMemory                 0.0    repliesShots               0.0
switchBackCost             0.0    themePickerShots           0.0    walk                       0.0

```

## 5. What one merged pool would be

Windows is the os that sets the split: its Go suite is the slowest and its jobs pay the most outside `ci.sh`.

| piece of the pool | windows seconds |
| --- | --- |
| Go, part 1 step as measured | 518 |
| Go, part 2 step with the duplicates gone (INFERRED, section 2) | about 350 |
| board, both halves, steps | 495 |
| ps-tests | 78 |
| together | about 1440 |

Per job on top of its tests: about 45 s outside `ci.sh`, 7 to 35 s machine and 24 to 27 s toolchain in a board job.

INFERRED from the table: four windows jobs of equal test time hold about 360 s each, near 7.5 minutes a job against
16.5 minutes for go-test-2 today. Two things bound it. gitsync whole is 416.6 s, so a job holding it cannot go below
that unless gitsync is cut by test name as daemon is. And Go packages and board shards each take all 4 cores, so in
one job they run one after the other and their times add.

Stage 2 is for whoever takes it: fix the exclusion in section 2 first, since that alone moves the run's end by more
than any rebalance, then pack the pool by the per-package and per-unit seconds above.
