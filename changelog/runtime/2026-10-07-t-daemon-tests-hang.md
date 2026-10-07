CI's go test runs with `-timeout 20m`. internal/daemon takes 550 to 600 s on a windows-latest runner, so the 10 minute
default killed it mid-run and the dump looked like a hang on a pty host attach. Nothing was stuck. A reattach no longer
leaks a goroutine per run that ended while no daemon was connected, the `--help` probe of a runner gives up 2 s after
its timeout even when a .cmd shim's child holds the pipe, and the daemon's tests never run a real runner's `--help`.
Item t-daemon-tests-hang.
