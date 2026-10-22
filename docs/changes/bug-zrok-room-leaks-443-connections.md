Docs: none, the design note docs/fabric/one-session-per-room.md and internal/link/AGENTS.md ship with the change

## Test plan

## @LETTER@. One session per room, on every transport

### @LETTER@1. A zrok room holds a flat number of sockets

1. Attach a test room over a private zrok share. Never sgg or sg3.
2. On the room's machine, count established connections to `:443` owned by the room's process, once a minute for ten
   minutes (`Get-NetTCPConnection -State Established -OwningProcess <pid> | Where RemotePort -eq 443`).
3. Restart the hub twice in that time.

**Expected:** the count stays at one per edge router the identity reaches, and the room log shows
`[ziti] context built (zrok), 1 live` once.

### @LETTER@2. A direct room holds one connection to the hub

1. Attach a test room over direct, then open its board, a terminal and an event stream through the hub.
2. On the hub, list established connections on the link port from the room's address, twice, 40 s apart.

**Expected:** one connection, with the same remote port in both samples.

### @LETTER@3. A hub restart reopens the one connection

1. Restart the hub that test room is on.

**Expected:** the room attaches again within a few seconds, and the hub shows one connection from it again, on a new
port.

### @LETTER@4. A new room on an old hub, and an old room on a new hub

1. Point a room built from this change at a hub built before it.
2. Point a room built before this change at a hub built from it.

**Expected:** both attach and serve the board. The new room logs once that the hub does not take a muxed link and dials
a connection per stream, as before.
