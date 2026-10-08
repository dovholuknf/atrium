## Test plan

## @LETTER@. A room's spec and lock on the hub

### @LETTER@1. Add a room with a spec

1. Write a `room.yaml` with `version: 1` and `name: sg3`.
2. Run `atrium rooms add sg3 --spec room.yaml`, then `atrium rooms spec get sg3`.

**Expected:** the room is added and `spec get` prints the file exactly as written.

### @LETTER@2. A bad spec is refused

1. Run `atrium rooms spec set sg3 -` with a spec that has `name: other`, then one with a `token:` key, then one with
   `version: 2`.

**Expected:** each is refused with a sentence and `spec get` still prints the earlier spec.

### @LETTER@3. A room pulls its own spec

1. On the attached room, run `atrium room spec pull`.

**Expected:** `room.yaml` is written beside the room's certificate and its sha256 matches the hub's.

### @LETTER@4. A lock is kept and never changes the spec

1. Post a lock from the room with the link function `cli.PostRoomLock`.
2. Run `atrium rooms lock get sg3` and `atrium rooms spec get sg3` on the hub.

**Expected:** the lock prints as posted and the spec is unchanged.

### @LETTER@5. A room cannot reach another room's spec

1. From room sg4, run `atrium room spec pull`.

**Expected:** it gets sg4's spec, or "the hub has no spec" for sg4, never sg3's.

### @LETTER@6. The board can read, not write

1. Open `/_hub/rooms/sg3/spec` and `/_hub/rooms/sg3/lock` on the board's address.
2. Send a POST to the same address.

**Expected:** the GETs answer JSON and the POST is refused with 405.

The board's read is board-visible by design, like the backlog's reads: the board is on loopback with no login, so anybody
who can reach it sees every room's spec and lock. A room with no spec and a room that does not exist answer the same 404.

### @LETTER@7. A room with no certificate is refused

1. From a room still on the old overlay path (no certificate), run `atrium room spec pull`.

**Expected:** the hub refuses with a sentence telling it to re-join, and no spec or lock is read or written.
