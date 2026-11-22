# qa-flake-win-tempdir-removeall

Windows `go test` failed at `t.TempDir` cleanup ("unlinkat ...: The directory is not empty") with the test body green.

`handleActivity` answers, then detaches `go d.onActivity(in)`. That goroutine writes to the store and the card dirs
and nothing waited for it, so it could still be writing when the test closed the store and removed the temp dir.

Now `handleActivity` counts the goroutine, `closeAtCleanup` waits for it beside the runner exits, and the cli test
daemon's `stop` calls `WaitActivity` before it closes. No timeout widened, no retry in cleanup.
