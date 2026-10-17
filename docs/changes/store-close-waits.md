Docs: none, no verb, flag, setting or file changed. `store.Close` waits for calls in flight.

## What and why

`internal/api.TestOpenAnIssueClaimsItsKeyAndRefusesOneAnotherRoomHolds` failed on the windows-latest runner (run
39032382613) in TempDir's cleanup: `t.db` was "being used by another process" two seconds after the store's Close,
which is as long as testing's RemoveAll retries on Windows.

The store opens sqlite with one connection. `database/sql.DB.Close` closes the connections that are free and
leaves one in use to be closed when its call releases it. So a store call that is mid-operation when Close runs
keeps the file open until its query ends and it is scheduled again. The one here was the measure `measureAfterOpen`
starts after a 201 open, inside `Resources` or `SetResourceBytes`, caught by a goroutine dump taken when a rename
of `t.db` failed right after Close. Under 24 CPU burners on this machine the file stayed held up to 655 ms after
Close in 6 of 40 runs, the same shape stretched past 2 s on 4 shared cores.

`Store.Close` now sets its closed flag, waits for the count of calls running an operation to reach zero, bounded at
10 s with a log line, and then closes the database. `guard` counts each operation attempt, not its retry sleeps, and
a call that arrives once the flag is set answers `ErrClosed` without touching the database. A new store test
hammers `Resources` from eight goroutines across Close and renames the file the moment Close returns: 5 of 5 failed
without the wait on Windows, 5 of 5 pass with it.

## Test plan

## @LETTER@. The store's file is free when Close returns

### @LETTER@1. The store test

1. Run `go test -tags integration -count=5 -run TestCloseReleasesTheFileWithCallsInFlight ./internal/store` on
   Windows.

**Expected:** passes every time.

### @LETTER@2. The api open test under load

1. Run `go test -tags integration -count=20 -run TestOpenAnIssueClaimsItsKeyAndRefusesOneAnotherRoomHolds
   ./internal/api` with the machine's cores busy.

**Expected:** no "TempDir RemoveAll cleanup" line.
