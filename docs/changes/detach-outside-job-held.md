Docs: none, a test fix and one error message.

## What and why

Run 39052523653, windows go-test-2: `TestStartOutsideJobAnswersTheProgramAndWritesItsLog` failed with
`OpenProcess: The parameter is incorrect` after 3.5 s. The test starts `ping -n 2 127.0.0.1` through WMI, which
takes about a second on an idle machine and 3 s or more on the loaded runner: the PowerShell script polls CIM for
cmd's child every 100 ms, and each query is slow under load. Two pings take about a second, so by the time the
script had printed the pid and `os.FindProcess` called `OpenProcess`, ping was gone, and Windows answers a pid that
no longer exists with that error.

The test now pings thirty times and kills the process in its cleanup, as it already did. `startOutsideJob` wraps a
failed `FindProcess` so the error names the program and the pid and says it had ended before it could be held, which
is what the bare `OpenProcess` text meant.

## Test plan

## @LETTER@. The outside-the-job start holds a live program

### @LETTER@1. The integration test

1. `go test -tags integration -run TestStartOutsideJob ./internal/detach/` on Windows, with the machine busy.

**Expected:** both tests pass. The first logs `started after` a second or more and still finds ping running.
