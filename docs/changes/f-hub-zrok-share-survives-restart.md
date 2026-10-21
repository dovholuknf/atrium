Docs: d-0, none, docs/fabric/room-over-zrok.md is updated in the branch

## Test plan

## @LETTER@. The hub's zrok share survives a restart

### @LETTER@1. A restart keeps the share

1. On a hub with a zrok environment, start it with `--transport direct,zrok`. Note the token in `<hub dir>/zrok-share`.
2. Add a room over zrok and join it. Restart the hub with `atrium stop` and start it again.

**Expected:** `zrok-share` holds the same token, the room reattaches with no new join string, and the hub log does not
say it reserved a share.

### @LETTER@2. A deleted share is replaced

1. Stop the hub, run `zrok delete share <token>`, start the hub.

**Expected:** the log says it reserved a private zrok share, `zrok-share` holds a new token, and `rooms token <name>`
mints a string that attaches.

### @LETTER@3. Retire

1. Run `atrium rooms retire-share --atrium-dir <hub dir>`.

**Expected:** the share is gone from `zrok overview` and `zrok-share` is removed.

### @LETTER@4. The gear row

1. Open the gear, "expose the board". Find `Room link over zrok`, with no service or identity box.
2. Press `turn off`, then `turn on`.

**Expected:** the row reads `off` then `bound`, and a room reaching the hub only over zrok makes the first press refuse,
naming the room.
