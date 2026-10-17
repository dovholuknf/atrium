Docs: none, a fix that makes the room match what it already says

## Test plan

## @LETTER@. One ziti context per identity

### @LETTER@1. The count is one and stays one

1. Run a room joined over ziti, and open the board's ziti settings a few times.
2. Stop the controller for two minutes, then start it again.
3. Read `ziti_contexts` from `/v1/room-stats` and count `[ziti] context built` lines in `room.log`.

**Expected:** `ziti_contexts` is 1 throughout, one `context built (link)` line per identity, and no `context closed`
line until the room stops.

### @LETTER@2. Commands borrow the room's link

1. With the room running, run `atrium room ssh sync` and `atrium rooms spec pull`.
2. Stop the room and run either again.

**Expected:** the first run works and builds no context. The second says no room is running on this machine, and builds
no ziti context either.
