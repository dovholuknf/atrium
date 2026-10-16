# ci on windows: FAILED

- windows, x86_64, go1.26.9, node v24.21.0, 4 cpus
- commit: 422541a5

## Steps

| step | seconds | result | tests run | tests failed |
|---|---|---|---|---|
| machine | 1 | ok | - | - |
| toolchain | 1 | ok | - | - |
| go-test | 460 | FAILED | 5285 | 15 |

## Failing tests

| step | test | first error |
|---|---|---|
| go-test | github.com/dovholuknf/atrium/internal/api TestCloseStopsAProcessTheCardOwns | cardports_integration_test.go:132: 200 {Code: Closed:true Left:1 Asks:[] Items:[{Seq:1 Kind:proc Ref:25676 Detail:1792173463581437700 Action:keep Note: Bytes:-1 Err:taskkill: ERROR: Access denied |
| go-test | github.com/dovholuknf/atrium/internal/api TestCloseStopsAProcessTheRoomStartedForTheCard | cardprocstart_integration_test.go:55: 200 {Code: Closed:true Left:1 Asks:[] Items:[{Seq:1 Kind:proc Ref:28816 Detail:1792173469917559600 Action:keep Note: Bytes:-1 Err:taskkill: ERROR: Access denied |
| go-test | github.com/dovholuknf/atrium/internal/api TestCloseStopsAndDeletesTheCardsOverlay | cardoverlay_integration_test.go:86: 200 {Code: Closed:true Left:1 Asks:[] Items:[{Seq:1 Kind:overlay Ref:C:/Users/claude/AppData/Local/Temp/TestCloseStopsAndDeletesTheCardsOverlay2013837013/005/01a145dc-a248-7a29-9aec-289a271c08e2/overlay/o1 Detail:https://127.0.0.1:1 Action:freed Note: Bytes:-1 Err |
| go-test | github.com/dovholuknf/atrium/internal/api TestOverlayDownStopsOnlyThatOne | cardoverlay_integration_test.go:109: 500 {"error":"process 23592 did not stop: taskkill: ERROR: Access denied\r\r\n"} |
| go-test | github.com/dovholuknf/atrium/internal/claudeconf TestALongerGateTimeoutIsLeftAloneAndWritesNothing | gate_integration_test.go:145: an already right gate was rewritten: {Changed:true Backup:C:/Users/claude/AppData/Local/Temp/TestALongerGateTimeoutIsLeftAloneAndWritesNothing991712236/001/.claude/settings.json.atrium-20261016-135727.bak} |
| go-test | github.com/dovholuknf/atrium/internal/claudeconf TestClaudeCommandStillQuotesAPathWithASpace | codex_command_integration_test.go:28: claude's command lost its quotes, so the shell will split it: & "C:/Program Files/atrium/atrium.exe" session --event start |
| go-test | github.com/dovholuknf/atrium/internal/claudeconf TestClaudeEntriesAreNotStaleForSayingNoRunner | codex_command_integration_test.go:227: an ordinary claude hook was marked as pointing elsewhere |
| go-test | github.com/dovholuknf/atrium/internal/claudeconf TestHookCommandQuotesAPathWithSpaces | hooks_integration_test.go:430: a spaced path was written unquoted: & "/opt/atrium bin/atrium" hook --event tool-start |
| go-test | github.com/dovholuknf/atrium/internal/claudeconf TestHookCommandSlashesAWindowsPath | hooks_integration_test.go:445: a windows path was not slashed: & "C:/Program Files/atrium/atrium.exe" hook --event tool-start |
| go-test | github.com/dovholuknf/atrium/internal/claudeconf TestInstallWritesThroughASymlink | preserve_integration_test.go:109: open C:\Users\claude\AppData\Local\Temp\TestInstallWritesThroughASymlink2739946629\001\.claude\settings.json: The path cannot be traversed because it contains an untrusted mount point. |
| go-test | github.com/dovholuknf/atrium/internal/daemon TestALaunchedClaudeStartsOnTheClassicRenderer | firstrun_integration_test.go:69: the runner never wrote its environment: open C:\Users\claude\AppData\Local\Temp\TestALaunchedClaudeStartsOnTheClassicRenderer2439427929\002\env.txt: The system cannot find the file specified. |
| go-test | github.com/dovholuknf/atrium/internal/daemon TestSaysQueuedBeforeACycleArriveOnceAfterTheWake | queued_say_cycle_integration_test.go:88: 1 messages still queued |
| go-test | github.com/dovholuknf/atrium/internal/gitsync TestPushToHubIgnoresAProxyTheClonesConfigSets | (no output) |
| go-test | github.com/dovholuknf/atrium/internal/gitsync TestPushToHubIgnoresAProxyTheClonesConfigSets/http.<base>.proxy | hubpush_integration_test.go:317: the push did not land: exit status 1 |
| go-test | github.com/dovholuknf/atrium/internal/store TestForcedFailureForCISummary | zz_forced_test.go:6: forced: want 1 got 2 |

## Failed packages (end of their output)

### github.com_dovholuknf_atrium_internal_api

```
2026/10/16 13:59:51 [atrium open] https://github.com/o/r/pull/7: worktree took 0.40s
2026/10/16 13:59:51 [atrium open] https://github.com/o/r/pull/7: review took 0.01s
2026/10/16 13:59:51 [atrium open] https://github.com/o/r/pull/7: card took 0.00s
2026/10/16 13:59:51 [atrium open] https://github.com/o/r/pull/7: worktree took 0.37s
2026/10/16 13:59:51 [atrium open] https://github.com/o/r/pull/7: review took 0.01s
2026/10/16 13:59:51 [atrium open] https://github.com/o/r/pull/7: card took 0.00s
2026/10/16 13:59:51 [atrium close] 01a145de-c8dd-7041-865e-fc03bfba7e37: session took 0.00s
2026/10/16 13:59:51 [atrium close] 01a145de-c8dd-7041-865e-fc03bfba7e37: review took 0.01s
2026/10/16 13:59:51 [atrium close] 01a145de-c8dd-7041-865e-fc03bfba7e37: worktree:1 took 0.09s
2026/10/16 13:59:51 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: session took 0.00s
2026/10/16 13:59:51 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: review took 0.11s
2026/10/16 13:59:51 [atrium close] 01a145de-c8dd-7041-865e-fc03bfba7e37: branches took 0.20s
2026/10/16 13:59:51 [atrium close] 01a145de-c8dd-7041-865e-fc03bfba7e37: refs took 0.08s
2026/10/16 13:59:51 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: worktree:1 took 0.09s
2026/10/16 13:59:51 [atrium close] 01a145de-c8dd-7041-865e-fc03bfba7e37: card took 0.01s
2026/10/16 13:59:51 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: session took 0.00s
2026/10/16 13:59:52 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: review took 0.10s
2026/10/16 13:59:52 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: branches took 0.16s
--- PASS: TestCloseDeletesWhenToldTo (3.07s)
2026/10/16 13:59:52 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: worktree:1 took 0.09s
2026/10/16 13:59:52 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: refs took 0.15s
2026/10/16 13:59:52 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: clone:4 took 0.00s
2026/10/16 13:59:52 [atrium close] 01a145de-cb35-73dd-86cc-7a1591d3dec6: card took 0.00s
2026/10/16 13:59:52 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: branches took 0.09s
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: session took 0.00s
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: review took 0.01s
--- PASS: TestCloseKeepsACloneSomethingElseUses/not_marked_as_made (2.71s)
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: worktree:1 took 0.10s
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: branches took 0.10s
2026/10/16 13:59:52 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: refs took 0.31s
2026/10/16 13:59:52 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: clone:4 took 0.00s
2026/10/16 13:59:52 [atrium close] 01a145de-ca09-7e26-b434-d68957ff1c7b: card took 0.00s
--- PASS: TestCloseKeepsACloneSomethingElseUses/another_worktree (3.50s)
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: refs took 0.34s
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: clone:4 took 0.00s
2026/10/16 13:59:52 [atrium close] 01a145de-ca7e-7c8c-9579-c193b7626871: card took 0.00s
--- PASS: TestCloseKeepsACloneSomethingElseUses/a_commit_on_no_remote (3.60s)
--- PASS: TestCloseKeepsACloneSomethingElseUses (0.00s)
FAIL
FAIL	github.com/dovholuknf/atrium/internal/api	145.999s
```

### github.com_dovholuknf_atrium_internal_claudeconf

```
--- PASS: TestInstallAllSkipsTheStopHook (0.04s)
=== RUN   TestStopHookInstallsWhenNamed
--- PASS: TestStopHookInstallsWhenNamed (0.02s)
=== RUN   TestAnUninstalledOptionalHookIsNotMissing
--- PASS: TestAnUninstalledOptionalHookIsNotMissing (0.00s)
=== RUN   TestAStaleOptionalHookStillCounts
--- PASS: TestAStaleOptionalHookStillCounts (0.02s)
=== RUN   TestInstallKeepsSiblingCommands
--- PASS: TestInstallKeepsSiblingCommands (0.04s)
=== RUN   TestInstallIsIdempotent
--- PASS: TestInstallIsIdempotent (0.09s)
=== RUN   TestInstallWritesThroughASymlink
    preserve_integration_test.go:109: open C:\Users\claude\AppData\Local\Temp\TestInstallWritesThroughASymlink2739946629\001\.claude\settings.json: The path cannot be traversed because it contains an untrusted mount point.
--- FAIL: TestInstallWritesThroughASymlink (0.01s)
=== RUN   TestBashFoundWritesNoShellKeyAndTheOldCommand
--- PASS: TestBashFoundWritesNoShellKeyAndTheOldCommand (0.02s)
=== RUN   TestNoBashWritesPowerShellShapeAndCallOperatorForAQuotedPath
--- PASS: TestNoBashWritesPowerShellShapeAndCallOperatorForAQuotedPath (0.02s)
=== RUN   TestStatusRecognisesThePowerShellFormAndFlagsAMissingShell
--- PASS: TestStatusRecognisesThePowerShellFormAndFlagsAMissingShell (0.04s)
=== RUN   TestFindGitBashLooksWhereClaudeCodeLooks
--- PASS: TestFindGitBashLooksWhereClaudeCodeLooks (0.00s)
=== RUN   TestHookExePrefersTheRunningDaemon
--- PASS: TestHookExePrefersTheRunningDaemon (0.02s)
=== RUN   TestHookExeFallsBackWhenNoDaemonHasRun
--- PASS: TestHookExeFallsBackWhenNoDaemonHasRun (0.01s)
=== RUN   TestHookExeFallsBackWhenTheFileHasNoExe
--- PASS: TestHookExeFallsBackWhenTheFileHasNoExe (0.02s)
=== RUN   TestHookExeIgnoresARecordedBinaryThatIsGone
--- PASS: TestHookExeIgnoresARecordedBinaryThatIsGone (0.02s)
=== RUN   TestHookExeIgnoresAnUnreadableLocationFile
--- PASS: TestHookExeIgnoresAnUnreadableLocationFile (0.02s)
=== RUN   TestHookExeHonoursTheOverride
--- PASS: TestHookExeHonoursTheOverride (0.02s)
=== RUN   TestHookExeUsesForwardSlashes
--- PASS: TestHookExeUsesForwardSlashes (0.01s)
=== RUN   TestDefaultLocationPathIsNotEmpty
--- PASS: TestDefaultLocationPathIsNotEmpty (0.00s)
FAIL
FAIL	github.com/dovholuknf/atrium/internal/claudeconf	1.840s
```

### github.com_dovholuknf_atrium_internal_daemon

```
=== CONT  TestScreenAgainstXterm/reverse_index_at_region_top
--- PASS: TestScreenAgainstXterm/CSI_S_in_a_region_below_the_top (0.22s)
=== CONT  TestScreenAgainstXterm/region_below_the_top_discards
--- PASS: TestScreenAgainstXterm/CSI_S_and_T_in_a_region (0.22s)
=== CONT  TestScreenAgainstXterm/scroll_region
--- PASS: TestScreenAgainstXterm/index_below_the_region (0.20s)
=== CONT  TestScreenAgainstXterm/full_reset
--- PASS: TestScreenAgainstXterm/reverse_index_at_region_top (0.19s)
=== CONT  TestScreenAgainstXterm/clear_scrollback
--- PASS: TestScreenAgainstXterm/region_below_the_top_discards (0.17s)
=== CONT  TestScreenAgainstXterm/region_reset_by_bare_r
--- PASS: TestScreenAgainstXterm/scroll_region (0.17s)
=== CONT  TestScreenAgainstXterm/region_ignored_when_invalid
--- PASS: TestScreenAgainstXterm/full_reset (0.17s)
=== CONT  TestScreenAgainstXterm/clear_screen
--- PASS: TestScreenAgainstXterm/clear_scrollback (0.17s)
=== CONT  TestScreenAgainstXterm/erase_display
--- PASS: TestScreenAgainstXterm/region_reset_by_bare_r (0.19s)
=== CONT  TestScreenAgainstXterm/reverse_index
--- PASS: TestScreenAgainstXterm/region_ignored_when_invalid (0.20s)
=== CONT  TestScreenAgainstXterm/save_restore_cursor
--- PASS: TestScreenAgainstXterm/clear_screen (0.20s)
=== CONT  TestScreenAgainstXterm/scroll_up_and_down
--- PASS: TestScreenAgainstXterm/erase_display (0.20s)
=== CONT  TestScreenAgainstXterm/insert_delete_lines
--- PASS: TestScreenAgainstXterm/reverse_index (0.20s)
=== CONT  TestScreenAgainstXterm/insert_delete_erase_chars
--- PASS: TestScreenAgainstXterm/save_restore_cursor (0.20s)
=== CONT  TestScreenAgainstXterm/tabs
--- PASS: TestScreenAgainstXterm/scroll_up_and_down (0.21s)
=== CONT  TestScreenAgainstXterm/autowrap
--- PASS: TestScreenAgainstXterm/insert_delete_lines (0.25s)
=== CONT  TestScreenAgainstXterm/width_shrinks_over_a_long_row
--- PASS: TestScreenAgainstXterm/insert_delete_erase_chars (0.27s)
--- PASS: TestScreenAgainstXterm/tabs (0.25s)
--- PASS: TestScreenAgainstXterm/autowrap (0.24s)
--- PASS: TestScreenAgainstXterm/width_shrinks_over_a_long_row (0.23s)
--- PASS: TestScreenAgainstXterm (0.01s)
FAIL
FAIL	github.com/dovholuknf/atrium/internal/daemon	439.569s
```

### github.com_dovholuknf_atrium_internal_gitsync

```
=== CONT  TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch/done_6_d
--- PASS: TestARepositorysOwnHookAndConfigAreNotUsed (7.14s)
=== CONT  TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch/idle
--- PASS: TestReftableOnlyWhereGitHasItAndTheProbeWorks/new_git,_reftable_repo_still_seeds_and_is_read (2.63s)
--- PASS: TestReftableOnlyWhereGitHasItAndTheProbeWorks (8.46s)
=== CONT  TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/dead
--- PASS: TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch/running (6.24s)
=== CONT  TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/done_365d
--- PASS: TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch/idle (6.58s)
=== CONT  TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/gone
--- PASS: TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch/done_6_d (6.71s)
--- PASS: TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch (0.00s)
=== CONT  TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/done_8_d
--- PASS: TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/dead (7.94s)
=== CONT  TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent/a_server_error
--- PASS: TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent/a_server_error (1.37s)
=== CONT  TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent/a_cut-off_advertisement
--- PASS: TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent/a_cut-off_advertisement (1.19s)
=== CONT  TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent/not_a_git_advertisement
--- PASS: TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/done_365d (7.62s)
=== CONT  TestPushToHubIgnoresAProxyTheClonesConfigSets/remote.hub.proxy
--- PASS: TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent/not_a_git_advertisement (1.06s)
--- PASS: TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent (0.00s)
=== CONT  TestPushToHubIgnoresAProxyTheClonesConfigSets/http.<base>.proxy
--- PASS: TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/gone (6.91s)
=== CONT  TestPushToHubIgnoresAProxyTheClonesConfigSets/http.<push>.proxy
--- PASS: TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch/done_8_d (6.92s)
=== CONT  TestPushToHubIgnoresAProxyTheClonesConfigSets/http.proxy
--- PASS: TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch (0.00s)
--- PASS: TestPushToHubIgnoresAProxyTheClonesConfigSets/remote.hub.proxy (4.71s)
    hubpush_integration_test.go:317: the push did not land: exit status 1
        To http://127.0.0.1:57245/git/hub/github/o/r.git
        *	refs/heads/fix/x:refs/heads/fix/x	[new branch]
        Done
--- FAIL: TestPushToHubIgnoresAProxyTheClonesConfigSets/http.<base>.proxy (4.74s)
--- PASS: TestPushToHubIgnoresAProxyTheClonesConfigSets/http.proxy (4.81s)
--- PASS: TestPushToHubIgnoresAProxyTheClonesConfigSets/http.<push>.proxy (4.84s)
--- FAIL: TestPushToHubIgnoresAProxyTheClonesConfigSets (0.00s)
FAIL
FAIL	github.com/dovholuknf/atrium/internal/gitsync	401.735s
```

### github.com_dovholuknf_atrium_internal_store

```
--- PASS: TestPathRuleCoversRelativeCommandsFromInside (1.35s)
=== CONT  TestNarrowPathRuleBeatsABroadOne
--- PASS: TestPathRuleCoversFileTools (1.37s)
=== CONT  TestPathRuleRefusesTheWholeMachine
--- PASS: TestPathAndCommandRulesCoexist (1.17s)
=== CONT  TestPathRuleIgnoresCase
--- PASS: TestRulesWithoutAKindStayCommandRules (1.21s)
=== CONT  TestPathRuleDoesNotMatchASiblingPrefix
--- PASS: TestPathRuleDoesNotMatchASiblingPrefix (1.45s)
=== CONT  TestPathRuleMatchesAQuotedPathMidCommand
--- PASS: TestPathRuleRefusesTheWholeMachine (1.59s)
=== CONT  TestMCPPrecedence
--- PASS: TestPathRuleIgnoresCase (1.54s)
=== CONT  TestMCPRuleIsIdempotent
--- PASS: TestNarrowPathRuleBeatsABroadOne (1.69s)
=== CONT  TestMCPRuleScope
--- PASS: TestPathRuleMatchesAQuotedPathMidCommand (0.92s)
=== CONT  TestAddMCPRuleValidation
--- PASS: TestMCPRuleIsIdempotent (1.04s)
=== CONT  TestMCPGlobIsNotACandidateForOtherTools
--- PASS: TestMCPRuleScope (1.02s)
=== CONT  TestEqualSpecificityBlockWinsWhicheverIsInsertedFirst
--- PASS: TestAddMCPRuleValidation (1.03s)
=== CONT  TestMCPExactRule
--- PASS: TestMCPGlobIsNotACandidateForOtherTools (1.28s)
=== CONT  TestMCPPrefixGlob
--- PASS: TestMCPExactRule (1.21s)
=== CONT  TestMCPServerGlob
--- PASS: TestEqualSpecificityBlockWinsWhicheverIsInsertedFirst (2.21s)
=== CONT  TestRuleUpsertAndForget
--- PASS: TestMCPPrefixGlob (0.99s)
=== CONT  TestRuleCountsHits
--- PASS: TestMCPServerGlob (1.31s)
=== CONT  TestAddRuleRejectsMatchEverything
--- PASS: TestRuleCountsHits (1.48s)
--- PASS: TestRuleUpsertAndForget (1.72s)
--- PASS: TestAddRuleRejectsMatchEverything (0.91s)
--- PASS: TestMCPPrecedence (7.41s)
FAIL
FAIL	github.com/dovholuknf/atrium/internal/store	183.703s
```

## Slowest 10 tests

| seconds | package | test |
|---|---|---|
| 88.420 | github.com/dovholuknf/atrium/internal/gitsync | TestAChunkedPostGoesThrough |
| 71.190 | github.com/dovholuknf/atrium/internal/link | TestAChunkedPostGoesThroughForwarderLinkAndCgi |
| 21.870 | github.com/dovholuknf/atrium/internal/gitsync | TestAnOperatorsPushedMainIsNeverOverwrittenByTheForge |
| 21.680 | github.com/dovholuknf/atrium/internal/store | TestOpeningSeparateStoresTogetherDoNotFailMidMigration |
| 19.140 | github.com/dovholuknf/atrium/internal/gitsync | TestABranchOnlyTheForgeHasIsFoundAndFetchableThroughTheHub |
| 18.370 | github.com/dovholuknf/atrium/internal/link | TestABranchTheRoomWouldNotServeIsNeverListed |
| 17.790 | github.com/dovholuknf/atrium/internal/gitsync | TestDeleteTagNotesAndStashFromACardAreRefusedAndNothingMoves |
| 17.150 | github.com/dovholuknf/atrium/internal/link | TestStashNotesAndAnUnservedBranchAreRefusedThroughTheHub |
| 16.480 | github.com/dovholuknf/atrium/internal/store | TestOpeningTheSameStoreTogetherNeverFailsMidMigration |
| 16.060 | github.com/dovholuknf/atrium/internal/gitsync | TestASeededMainFollowsItsForge |

## Reproduce

```
bash scripts/ci.sh
bash scripts/ci.sh --only go-test
```

Raw files are in the run's artefacts (build.claude/ci/).
