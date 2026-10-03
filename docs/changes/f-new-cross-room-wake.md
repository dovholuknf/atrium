## Test plan

## @LETTER@. Waking a parked card on another room (f-new-cross-room-wake)

Needs the hub and the SENDING room built from this change and restarted. The target room can be any version. Use a
sender on m1mini and a target on claude-sg4 that you can park.

### @LETTER@1. A say with wake resumes a parked card on another room

1. Park a card on claude-sg4 (idle, no process). Note its handle.
2. From a card on m1mini, `atrium_say` to `<handle>@claude-sg4` with `wake: true` and a short text.

**Expected:** `delivered` is `queued` or `terminal`, not `parked`. The card on sg4 is no longer parked and shows the text
from `<you>@m1mini`.

### @LETTER@2. Without wake it is still refused

1. Park the card again, then `atrium_say` the same text with no `wake`.

**Expected:** `delivered: parked` with the "send again with wake=true" warning. The card stays parked and nothing is queued.

### @LETTER@3. A card that is not parked is not resumed

1. With the card running, `atrium_say` to it with `wake: true`.

**Expected:** delivered as a plain say would be. Nothing in its events says it was resumed.

### @LETTER@4. A room that is not attached refuses, and nothing is held

1. Stop the sg4 room, wait for the hub to show it detached, then `atrium_say` to `<handle>@claude-sg4` with `wake: true`.

2. Do it again as a card on the hub's own control (`atrium_say` on the hub tool), not from a room.

**Expected:** an error that names claude-sg4 and says it is not attached, and that nothing was sent or held. In 2 it is
an error too, never `unconfirmed`. Start sg4 again and nothing arrives by itself.

### @LETTER@4b. A slow resume reads unconfirmed, and lands

1. Make the resume slower than 8 seconds (a Windows target with a cold runner), then `atrium_say` with `wake: true` from
   the hub's tool.

**Expected:** the answer may be `unconfirmed` while the card does resume and gets the text. Nothing is sent twice. Ask
before sending again.

### @LETTER@5. The same from a room's own tool

1. From a card on m1mini, call the room's `atrium_say` (the stdio control) with `wake: true` to the parked card, then
   run `atrium tell --wake <handle>@claude-sg4 "text"` from a terminal on m1mini.

**Expected:** both resume the card and deliver.

### @LETTER@6. The schema

1. Read `atrium_say` in the tool list of the hub and of a room.

**Expected:** the `wake` description no longer says "local only".

### @LETTER@7. Headless

1. Run `env -u ATRIUM_LOCATION go test ./internal/link ./internal/cli ./internal/daemon` with
   `-run 'Wake'`.

**Expected:** pass.
