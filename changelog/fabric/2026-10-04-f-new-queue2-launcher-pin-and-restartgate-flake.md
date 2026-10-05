A test now pins link's `humanLauncher` to `store.HumanLauncher`, so the two cannot drift apart. The restart gate's
`TestAPatientAskMadeWhilePausedWaitsForResume` runs on a stopped clock, since after the resume it needs 70ms of idle
window and countdown inside a 100ms wait and a loaded machine can eat the spare and give `busy`. Test only, the gate is
unchanged. Item f-new-queue2-launcher-pin-and-restartgate-flake.
