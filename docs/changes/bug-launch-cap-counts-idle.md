Docs: none, a fix to what the cap counts and no reader-facing name changes

The test plan section BR expects a worker waiting for a message to hold a slot. Such a worker is idle at needs-input,
so under this change it does not count. The section below replaces that expectation.

## Test plan

## @LETTER@. The launch cap counts only mid-turn workers

Go tests `TestLaunchCapCountsOnlyRunningSubagents` and `TestLaunchRefusesAtTheCap` in
`internal/link/control_mcp_integration_test.go` cover the count. Needs the hub built from this change and a hub
restart.

### @LETTER@1. An idle worker does not take a slot

1. Start the hub with `ATRIUM_LAUNCH_CAP=2`.
2. From a session, `atrium_launch` two workers tagged `atrium:subagent`, each with a brief that says: run
   `sleep 600` in the foreground.
3. While both are mid-turn, launch a third the same way.
4. Interrupt one of the first two so its card sits at needs-input, then launch another tagged worker.

**Expected:** the third launch is refused with `at the launch cap of 2 running workers`. The last launch proceeds,
because a worker idle at needs-input does not count.

### @LETTER@2. A worker held at a permission prompt still counts

1. With the cap at 2, have two tagged workers mid-turn, one of them stopped at a permission prompt (needs-permission).
2. Launch a third tagged worker.

**Expected:** refused at the cap. A permission prompt holds a turn open, so the worker still counts.
