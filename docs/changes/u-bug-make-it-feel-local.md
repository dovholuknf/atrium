## Test plan

## @LETTER@. A room on the hub's machine uses loopback

### @LETTER@1. The rung order

1. On the hub's machine, join a room with `127.0.0.1:7779` and give it a ziti identity.
2. Run `atrium room transports` and see the loopback direct rung first, ziti second.
3. Open the Rooms tab and see the room `via direct`, and `GET /v1/health` through the hub answer in single digit ms.

### @LETTER@2. The input lag line

1. Tick input lag under the terminal's debug, type in a card of a room named in the rooms list.
2. See `wire N.Nms (room via transport)` on each key line and `last N keys (room via transport)` in the summary.
