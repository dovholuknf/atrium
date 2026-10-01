# Review: provision macOS start branch 755e967a (@fabric)

Range dafd4a5c..755e967a, one commit on claude/fabric: scripts/provision-room.ps1 (the macOS `start` step) and a
changelog line. Room side.

## What holds

- The order is right. A loaded LaunchAgent is `ok`. Otherwise a room that answers health on 7781 is `ok` with the
  reboot caveat. Otherwise the script starts `room --detach` and reports `done` or `warn`.
- `room --detach` (internal/cli/roomdetach.go) refuses to start a second room when one answers, and returns only once
  the new room answers or `detachWait` passes. The script's health check right after it is therefore meaningful.
- `--db "$HOME/.atrium/atrium.db"` is `daemon.DefaultDBPath()` on a Mac, and 7781 is `defaultRoomHTTP()`, the same
  port the Windows branch checks.
- `zsh -l` gives the room a login shell's PATH, so the runners it starts find `claude`. `exec "$0"` with `$Bin` as
  `$0` quotes the binary path.
- @fabric ran the detached-start branch on m1mini (`start=done started detached`). The "room answers" branch is
  untested in its new form, but it is one `curl` and an `echo`.

## Findings

### 1. LOW: the detached start's output is thrown away

`>/dev/null 2>&1` drops the line `room --detach` prints on failure, which names the pid and the log file. The `warn`
line then says only "did not answer". Keep the last line, `o=$(... 2>&1)` and append `${o##*$'\n'}` to the warn.

### 2. LOW: a later desktop login starts a second room against the first

When someone logs in at the desktop, launchd loads the agent and runs `room` without `--detach`. It finds 7781 taken
by the detached room and exits, and with KeepAlive set to restart on a bad exit, launchd restarts it every 10 seconds
for as long as the detached room runs. Nothing breaks, since the detached room keeps serving, but the log fills.
Either note it in the `done` line ("log out and back in, or reboot, to hand the room to the LaunchAgent"), or have
`runRoom` exit 0 when a room already answers at its address.

## Verdict

OK, room-ok dafd4a5c..755e967a. Two lows, no hold.

Quality: after the Sonnet switch, no drop seen. The correction about which branch ran is the right habit. The lows
are what happens after the step, not in it.
