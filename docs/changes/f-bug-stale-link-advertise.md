## Test plan

## @LETTER@. A hub that moved is said out loud and a room can follow it

### @LETTER@1. The hub warns about a stale advertised address

1. Start a hub with `--link-advertise` set to an address no interface on that machine has.
2. Read the hub's log.

**Expected:** a `[hub] WARNING` line names the address and why it is not this machine. The hub still starts.

### @LETTER@2. A room that cannot dial says so

1. On a direct room, point `room.json` at an address nothing answers on and start the room.
2. Open the room's own board (127.0.0.1:7781) and the Rooms tab.

**Expected:** "Not attached to its hub", the address it tried and "no answer within" are on the pane. It is gone once attached.

### @LETTER@3. Changing the hub address

1. Run `atrium room hub` on that room, then `atrium room hub <right host:port>`, then restart the room.

**Expected:** the first prints the old address, the second prints the new one and the backup's path
(`room.json.bak-<time>`), and the room attaches after the restart.
