# t-speed-redo: t-parallel and t-tests-fast redone on today's tests

Sources: hub claude/t-parallel (5c0c0f9f, e1d90765, d65e369d), hub claude/t-tests-fast (37fc3eed, a2eef03f) and the
untracked scripts/test-guard.ps1 on hub claude/wip-t-tests-fast. Each was read, not cherry-picked.

## t-parallel: nothing to port

The branch added `testguard.Parallel` (a no-op unless `-tags parallel`) and marked 227 daemon tests, leaving 1,390
serial because `daemon.New` wrote about 30 package-level hooks in `api`. Its own report measured daemon at 166.7s serial
and 165.6s parallel.

Main has moved past that. `internal/daemon` already has 1,263 plain `t.Parallel()` calls, and the a2a, overlay and
telemetry groups use them, `testDaemon` tests included. So `testguard.Parallel` and the `parallel` tag are not ported:
a second spelling of `t.Parallel()` with an opt-in tag would only hide tests from the default run.

What was left serial in those groups, and why:

- `TestAnUnverifiedShaIsAcceptedAndFlagged`, `TestAVerifiedShaIsNotFlagged`, `TestOnlyAnAgentLaunchedClaudeGetsTheStopHook`
  (a2a): I tried marking them parallel. `go test -tags integration -race` on them reported a DATA RACE (a database/sql
  goroutine), and the full daemon run failed. Reverted; they stay serial. Not chased further.
- `TestZrokEnvReportsNothingWhenThereIsNothing`, `TestZrokEnvReadsAnEnabledEnvironment`, `TestZrokEnvNeverCarriesTheToken`
  (overlay_setup): `t.Setenv("HOME"/"USERPROFILE")`, which panics in a parallel test and is process-wide.

## t-tests-fast: part already on main, the rest ported

- `-short` skips: main already has them on 775 tests. Added one more place, `gitSlot` in
  `internal/link/link_integration_test.go`, which every git-fixture rig calls (`newGitFixture`, `newPassRig`,
  `newPushFix`, the store rig). Under `-short` all of them skip, with the same message style; the full set still runs them.
- Fixture copies: main has `rigsnap_test.go` for the pass and url rigs. The three rigs it did not cover now copy a
  once-built template (`internal/link/template_test.go`, `fromTemplate`): `newGitFixture`, `newPushFix`
  (through `newPushFixFrom`) and the change-request rig (`newCRRig`, its four-commit history). Templates live under
  the snapshot roots and are removed by `removeSnapshots`. A copy is private to its test.
- `scripts/test-guard.ps1`: not adopted. It checks `go test -short` per-package and per-test limits with `// slow-ok:`
  markers; none exist on main, and `scripts/test.sh` already has the unit budget. It is not part of the two commits' idea.

## Numbers

Same machine (20 cpus), `go test -tags integration -count=1`, other workers running; link and api were run in one call.

| | before | after |
| --- | ---: | ---: |
| internal/link | 86.8s | 79.3s |
| internal/api | 64.0s | 49.8s (56.7s on a second run) |
| internal/daemon | 279.2s | 468.7s (machine loaded; no daemon test changed in the end) |
| internal/link `-short` | 61.2s | 44.9s |
| internal/api `-short` | | 44.4s |
| internal/gitsync `-short` | | 2.7s |

Daemon is not a result: no daemon file changed, so the difference is load. The link gain is small because the
fixture builds were already shared for pass and url rigs; the `-short` gain (about 16s) is the point.

## Tests

- `go test -tags integration -count=3 ./internal/link`: ok, 225s.
- `go test -tags integration -race -run 'CR|ChangeRequest|Push|Receive|GitFixture|Git' ./internal/link`: ok.
- `go test -count=1 ./internal/link ./internal/daemon` (no tag): ok.
- `go test -tags integration ./internal/daemon` (marks reverted): ok. `./internal/api`: ok.
