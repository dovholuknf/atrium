## Test plan

## @LETTER@. A killed runner leaves no children

### @LETTER@1. Kill a runner that has children

1. Launch a runner on a card and start a long process from it, for example `ping -t 127.0.0.1` on Windows.
2. Stop the runner from the board until it has to be killed, or end it with Terminate.
3. Confirm the ping process, and any node or ssh the runner started, are gone from the process list.

### @LETTER@2. A room restart keeps runners

1. With `pty_host` on, restart the room with `atrium stop` and start it again.
2. Confirm the runner and its children are still running.
