## Test plan

## @LETTER@. A room reattaches on its first try after a hub restart

### @LETTER@1. Restart the hub with rooms attached

1. With two or more rooms attached, restart the hub (hub-only deploy, no room restart).
2. Watch each room's log and the hub's `hub.err`.

**Expected:** each room logs one reattach within a few seconds. No room logs `the hub refused this room:` with an
empty reason, and `hub.err` shows one `room "<name>" attached` per room, not one every 5 seconds. The input-lag switch
reaches each room without "no room is attached".

### @LETTER@2. Provision a room

1. Run `pwsh -NoProfile -File scripts/provision-room.ps1 <host> ...` against a remote machine.

**Expected:** the attach step passes on the first dial, well inside `-AttachTimeout`.
