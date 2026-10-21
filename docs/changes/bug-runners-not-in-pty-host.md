## Test plan

## @LETTER@. A launched runner lives in the pty host

### @LETTER@1. A remembered host failure does not move runners into the room

1. Turn the `pty_host` setting on and make the pty host fail once, for example by killing it during a launch.
2. Launch a runner from the board.

**Expected:** the launch tries the pty host again and the runner lives in the host, not in the room's process.

### @LETTER@2. An unreadable job state starts through WMI

1. On Windows, run `Get-StartRoute` from `scripts/live` where the job state cannot be read.

**Expected:** it chooses the WMI route and does not treat the unreadable state as no job.
