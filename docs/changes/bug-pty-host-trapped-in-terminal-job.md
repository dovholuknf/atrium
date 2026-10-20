Docs: none, a fix that makes the start scripts and the refusal match what `docs/terminal/ptyhost-protocol.md` already says

## Test plan

## @LETTER@. The pty host is not trapped in the terminal's job

Run these on sg4 from a Windows Terminal pwsh, not from a shell under a room.

### @LETTER@1. A start from a terminal leaves no job over the room

1. Run `pwsh scripts/live/test-start-jobless.ps1`. Every line passes.
2. Stop the room (`atrium stop --runners` is fine, runners end once), then run `start-atrium-room.ps1` from the tab.
   The log line says `room started as pid N (route wmi), in no job object`.
3. `GET http://127.0.0.1:7781/v1/preflight`. `pty_host.in_job` is false once the host is up, and `pty_host.build` is the
   room's version line, not empty.
4. Close the terminal tab. The room and its pty host are still running.

### @LETTER@2. A hand-run room still gets a pty host outside the job

1. In the tab, run `atrium room` by hand (own dir, db and ports) with `pty_host` on.
2. `GET /v1/preflight` on it. `in_job` is false even though the room itself is in the tab's job. If it is true,
   `ptyhost.log` in its state dir and the room's log carry the `could not be started outside the job object` line with
   the WMI error.

### @LETTER@3. Startup waits for the host

1. With `pty_host` on and cards that had runners, restart the room. The log says `waiting for the pty host before
   reopening runners` or `the pty host is connected`, and then `reopening N terminal(s)`.
2. `GET /v1/preflight` right after. `runners.in_process` is 0.
3. To see the fallback, make the host unable to start (rename `atrium-pty.exe` aside and block its copy) and restart.
   After 45 seconds the log says `WARNING pty_host is on and no host answered`, and each reopened card has a
   `notified` event with `by` `pty-host-fallback`.

### @LETTER@4. A refused stop is plain

1. With a host that is in a job and at least one runner in it, run `deploy-batch.ps1` without `-KillRunners`.
2. **Expected:** it stops before any hold, says the host is inside a job object that refused breakaway, inherited from
   the process that started the room (with its pid), says to restart the room once from a jobless start, and still
   names `-KillRunners`. `atrium stop` says the same in its 409.
