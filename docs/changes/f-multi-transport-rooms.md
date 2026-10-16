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

## @LETTER@. Per path state on the rooms tab

### @LETTER@1. Badges for each transport

1. Attach a room that has a direct rung and a ziti rung, with the hub listening on both.
2. Open the rooms tab.

**Expected:** the room shows `direct`, `ziti` and `zrok` badges in that order. The one the control connection is on is
solid, the other that answered a probe is outlined, and a kind the room has no rung for is grey and italic. Hover for the
reason. Within about 30 seconds of stopping the hub's ziti listener, the ziti badge goes grey (unlistened).

### @LETTER@2. A path that dies

1. On a room attached over direct with a ziti rung, block the ziti service.

**Expected:** within about a probe (30 seconds, plus the 10 second dial bound) the ziti badge turns the warning colour and
its tip carries the dial error.

### @LETTER@3. The hub's listeners

1. `GET /v1/health` on the hub.

**Expected:** `listeners` holds `{kind, up, since, last_error}` for each side, and a side that could not bind is `up:false`
with its error. An older hub simply has no `listeners`, and an older room has no `paths` on its row and no badges.
