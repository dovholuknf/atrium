## Test plan

## @LETTER@. A held say survives a room restart

### @LETTER@1. Say held, room restarted, line cleared

1. With `pty_host` on, restart the room so a runner reattaches and its typing gate says "not sure what is on the line".
2. From another session, say two things to it with `atrium_say`. Both answer `queued`.
3. Restart the room again, then press Enter in the runner's terminal.

**Expected:** within about a minute both says are typed, once each and in order, and the card no longer shows a held
chip.

### @LETTER@2. Force while held

1. With the gate unsure, send two says, then a `force` say from the launcher.
2. Press Enter in the terminal.

**Expected:** the forced say types at once, and the two held says follow once each, in order.
