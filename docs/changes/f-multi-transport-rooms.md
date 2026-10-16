Docs: none, covered by docs/fabric/multi-transport-design.md and the flag help

## Test plan

## @LETTER@. A hub tells rooms a set of addresses

### @LETTER@1. A host name in the advertise

1. Start a hub with `--link-advertise hub.example:7779,10.0.0.5:7779`.
2. Join a room and read `transports.json`.

**Expected:** the hub list holds both direct addresses, in that order, and the join string carries the first.

### @LETTER@2. A hub that moves

1. Bind the hub wide with `--link 0.0.0.0:7779 --link-advertise <ip>`. Attach a room.
2. Change the machine's address.

**Expected:** within about 30 seconds the hub logs that the addresses changed, and the room logs
"the hub's transports changed" without reattaching.
