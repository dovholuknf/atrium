Docs: none, the design note docs/fabric/one-session-per-room.md and internal/link/AGENTS.md ship with the change

## Test plan

## @LETTER@. One session per room, on every transport

### @LETTER@1. A zrok room holds a flat number of sockets

1. Attach a test room over a private zrok share. Never sgg or sg3.
2. On the room's machine, count established connections to `:443` owned by the room's process, once a minute for ten
   minutes (`Get-NetTCPConnection -State Established -OwningProcess <pid> | Where RemotePort -eq 443`).
3. Restart the hub twice in that time.

**Expected:** the count stays at two per edge router the identity reaches (the data and control contexts), and the
room log shows two `[ziti] context built (zrok)` lines, once each.

### @LETTER@2. A direct room holds two connections to the hub

1. Attach a test room over direct, then open its board, a terminal and an event stream through the hub.
2. On the hub, list established connections on the link port from the room's address, twice, 40 s apart.

**Expected:** two connections, control and data, with the same remote ports in both samples.

### @LETTER@3. A hub restart reopens both connections

1. Restart the hub that test room is on.

**Expected:** the room attaches again within a few seconds, and the hub shows two connections from it again, on new
ports.

### @LETTER@4. A new room on an old hub, and an old room on a new hub

1. Point a room built from this change at a hub built before it.
2. Point a room built before this change at a hub built from it.

**Expected:** both attach and serve the board. The new room logs once that the hub does not take a muxed link and dials
a connection per stream, as before.

### @LETTER@5. A big git fetch does not cost a beat

1. Attach a test room on a slow uplink, or throttle its machine's upload to about 256 KiB/s.
2. Have the hub fetch a large repository from the room (a card's worktree with a few hundred MB of history), and
   download a large file from a card through the board at the same time.
3. Watch the room's last beat on the hub (`atrium rooms`, or the rooms list on the board) for the whole transfer.

**Expected:** the last beat never falls more than a beat or two behind, the room stays attached the whole time, and
the hub log shows no `went quiet` line for it.
