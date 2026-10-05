# f-new-queue2-launcher-pin-and-restartgate-flake

## Done
- (a) `TestHumanLauncherIsTheStoresHumanLauncher` in `internal/link/everywhere_test.go` pins `humanLauncher` to
  `store.HumanLauncher`.
- (b) `TestAPatientAskMadeWhilePausedWaitsForResume` now runs on a stopped clock (`stopClock`, the file's own idiom) and
  advances it 10ms at a time after the resume. Test only, the gate is unchanged.
- QUEUE.md item 2 (a) and (b) marked BUILT, (c) untouched. Changelog written.

## The flake did NOT reproduce
Counts run on the unfixed test: `-count=30` pass, `-count=50 -parallel 1` pass, `-count=50` with 2x NumCPU busy-loop
jobs pass. So the cause is by analysis, not observed. After the resume the ask needs the idle window (50ms) and the
countdown (20ms) inside a 100ms wait, on the real clock. 30ms of spare is all that stands between the test and `busy`,
and a timer overshoot of that size gives it (the check is `now >= deadline` at the top of the loop). The gate's logic
reads right. Hence this is reported incomplete: the fix is plausible and unproven against the real failure.

## Verify
- `go test ./internal/link -run 'TestAPatientAskMadeWhilePausedWaitsForResume|TestHumanLauncher' -count=50` passes.
- `-race` not run: the toolchain has no cgo here.
- Full `./internal/link`: one failure, `TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess`, in code I did not touch.
  I did not compare it to a clean claude/main run, so check it by name there.
