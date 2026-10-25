Docs: docs/fabric/room-over-zrok.md says a running hub binds again after its share goes

## Test plan

## @LETTER@. A running hub rebinds when its share is deleted

### @LETTER@1. Retire under a running hub

1. Start `atrium run --transport zrok` on a scratch directory.
2. Run `atrium rooms retire-share` against the same directory.

**Expected:** the command says each zrok room must rejoin. Within a minute the hub log says `the room listener over zrok
stopped, binding the share again`, then `reserved a private zrok share` and `listening for rooms over zrok`. A new
token is in `zrok-share`, and a string from `rooms token` joins.
