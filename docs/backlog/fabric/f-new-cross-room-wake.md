# f-new-cross-room-wake

Status: built on claude/f-cross-room-wake, awaiting review. `atrium_say` with `wake=true` to a card on another room
resumes it, through the existing relay `say` op (a new `wake` field, no new op), and delivers.

- Needs a room deploy on the sending room (it carries the field) as well as the hub. A sender room that is not rebuilt
  drops the flag and the target answers `parked` as before.
- Not done: the cold-start warning. The local wake gives none beyond the tool description, so neither does this.
- A wake say is refused, not held, when the hub or room cannot be reached: the outbox row has no wake and a column for
  it would be a migration.
