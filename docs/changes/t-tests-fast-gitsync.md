# t-tests-fast-gitsync: internal/gitsync tests faster

Measured on m1mini (macOS, 8 cores), `go test -tags integration -count=1 ./internal/gitsync`.

| Run                | Before | After     |
|--------------------|--------|-----------|
| Wall time, plain   | 51.4s  | 46 to 52s |
| Wall time, `-race` | 55s    | 51 to 53s |

The package was already under 60s here (189s on the loaded Windows box), so the gain on this box is small. The
parent-test times below are what each test reported before. After the change the subtests run in parallel, so the
parent reports about 0 and the cost shows in the package wall time.

| Test                                                | Before | After | Cause                              |
|-----------------------------------------------------|--------|-------|------------------------------------|
| TestAnOwnerWhoIsGoneDeadOrLongDoneReleasesTheBranch | 9.2s   | ~0    | four rigs built one after another  |
| TestPushToHubIgnoresAProxyTheClonesConfigSets       | 6.7s   | ~0    | four rigs built one after another  |
| TestAnOwnerWhoIsLiveOrRecentlyDoneKeepsTheBranch    | 6.2s   | ~0    | three rigs built one after another |
| TestAHubStoppedMidFetchLeavesEveryRefWhole          | 4.6s   | 1.8s  | fixed 3s pause before the stop     |

Every other test is 2 to 4s under load and is dominated by git subprocesses (clone, push, receive-pack) run in
parallel across the package. Nothing left in the package is a timer or a timeout wait of note. The floor is the
total git work divided by the cores.

## Changes

- The three subtest loops build their own rig per case, so each case now calls `t.Parallel()`.
- TestAHubStoppedMidFetchLeavesEveryRefWhole waits for the stalled upload-pack request to arrive, in place of a
  fixed 3s pause, and fails after 30s if it never does.
- TestTheStoreRootIsTheSettingAndTheDefaultIsUnderTheHubDir switches the setting through an atomic. Under `-race`
  it raced with the background view rebuild.

## Production seam

One, for tests only: `Store.skipInitRefresh` (store.go). When set, `Init` does not start the background view
rebuild. `newStore` in the tests sets it, because the rebuild read `Store` fields that tests rewrite after `Init`
and `-race` flagged it intermittently. The default is false, so production is unchanged. The tests that exercise the
rebuild call `RefreshView` themselves.

## Test plan

No manual steps. Run `go test -tags integration -count=1 ./internal/gitsync` and once with `-race`.
