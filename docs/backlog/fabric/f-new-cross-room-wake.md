# f-new-cross-room-wake

Status: built on claude/f-cross-room-wake, awaiting review. `atrium_say` with `wake=true` to a card on another room
resumes it, through the existing relay `say` op (a new `wake` field, no new op), and delivers.

- Needs a room deploy on the sending room (it carries the field) as well as the hub. A sender room that is not rebuilt
  drops the flag and the target answers `parked` as before.
- Not done: the cold-start warning. The local wake gives none beyond the tool description, so neither does this.
- A wake say is refused, not held, when the hub or room cannot be reached: the outbox row has no wake and a column for
  it would be a migration.

Review 54af8c02 (M1, L1-L3), fixed in a second commit:

- A wake say the room cannot relay is refused with a 424, not a 503. The hub's own `atrium_say` reads 502, 503 and 504
  from the sender's room as "it may still deliver", so a 503 told the sender to wait on a parked card. 424 (the hub or
  room the request depends on failed) is used by nothing else for an in-flight message, and 409 is already this door's
  "two cards match".
- A wake say answered `parked` means the hub dropped the wake (an older hub): refused with a 424 that says the hub is
  too old, not "send again with wake=true".
- `linkRelay.Say` carrying `wake` is covered by a cli test against a real hub.
- Known: the hub's post to the target is bounded by `controlTimeout` (8 s) and the room unparks, which launches the
  runner, inside it. A resume slower than that lands but is reported `unconfirmed`. Safe (nothing is sent twice), so
  documented, not queued-before-unpark.
