Docs: none, a fix that makes the room match what it already says

## Test plan

## @LETTER@. A room holds one ziti context

### @LETTER@1. The count stays flat

1. Run a room joined over ziti for ten minutes with its ssh switch on.
2. Count `[ziti] context built` lines in `room.log`.

**Expected:** no new `context built (link)` line appears after the first one per rung, and `/v1/room-stats` reports a
flat `ziti_contexts`.

### @LETTER@2. The cap is loud

1. Read `room.log` on a room that has been up a day.

**Expected:** no `[ziti] refusing a context` line. Seeing one means something is leaking again.
