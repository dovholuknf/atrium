## Test plan

## @LETTER@. The board shows a zrok hub's share and env

### @LETTER@1. The hub's zrok row

1. Start `atrium run --transport zrok` on a scratch directory, on a machine with `zrok enable` done.
2. Open the gear on the hub's board and find the hub transports list.

**Expected:** a `Room link over zrok` row reads `bound`, has no switch, says it is the room link's first transport,
and shows `share abcd..., env <identity> at <api endpoint>` with only the first four characters of the token from
`zrok-share`. Retire the share with `atrium rooms retire-share` and the row reads `failing` or `binding` until the hub
rebinds.

### @LETTER@2. The room's tile

1. Join a room to that hub over zrok.
2. Open the Rooms page and hover the room's zrok badge.

**Expected:** the tip ends with `(share abcd...)`, the first four characters of the token the room joined with, never
the whole token.
