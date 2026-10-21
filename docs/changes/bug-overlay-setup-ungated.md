Docs: none, a fix that gates two routes

## Test plan

## @LETTER@. Overlay setup is local only

### @LETTER@1. From the room's own board

1. Open the room's board on 127.0.0.1:7781 and run an overlay setup.

**Expected:** it works as before.

### @LETTER@2. Through the hub or a proxy

1. `curl -X POST http://127.0.0.1:7778/v1/overlays/zrok/setup`.
2. Repeat with `-H 'X-Forwarded-For: 1.2.3.4'` against the room's port.

**Expected:** both answer 403 with "set up overlays from the machine they are for".
