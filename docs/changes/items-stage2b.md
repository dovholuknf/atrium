## Test plan

## @LETTER@. Items, stage 2b

Stuck alerts and one-time re-delivery for items, on top of `docs/changes/items-stage2a.md`. The design is
`docs/rnd/atrium-items-decisions.md` section 3. The timers are in `internal/daemon/item_stuck.go` and run on the
reaper's tick, so they have a granularity of about 20 seconds.

Which code is which, because it decides the deploy:

- **Hub code:** none. Everything here is read from, and written to, the room's mirror.
- **Room code:** `internal/store` (migration `0099_item_stuck`, the marks in `item.go`), `internal/daemon`
  (`item_stuck.go`, the hooks in `item_hub.go`, `reaper.go`, `a2a.go`, `newcontext.go`, `contextlog.go`). The room needs
  a restart to pick up the migration.
- **Board code:** none. The alert is the card's existing stuck flag and an event on the card.

### @LETTER@1. The owner never answers

1. Have an item worker block, owned by a session. Wait 15 minutes without answering.
2. Repeat, but end the owner's session right after the block.

**Expected:** 1. The worker's card shows stuck, "blocked for 15 minutes with no answer from its owner", and one
`item-stuck` event is on its history, once however long you wait. 2. The same alert comes on the next tick, saying the
owner has no session. An item owned by the human has no timer.

### @LETTER@2. The worker does not resume

1. Unblock a blocked item, and keep the worker's terminal from starting a turn (a held-open permission prompt works).

**Expected:** two minutes after the answer is delivered it is typed once more, led by "in case you missed it". Two
minutes after that the card shows stuck, "its owner answered and the worker has not picked the answer up", once. The
item is still `blocked`. A worker that resumes in time gets neither.

### @LETTER@3. A context clear

1. Block an item, let the owner's notice be delivered, then run a new context on the owner before it acts.
2. Unblock an item, let the answer be delivered, then run a new context on the worker before it resumes.

**Expected:** after the wake, the notice (or the answer) comes once more, led by "in case you missed it". A second
clear does not hand it back again, and an item that has moved on is not handed back at all.

### Decisions where the spec was ambiguous

- "Alerts the human" is an `item-stuck` event on the worker's card plus the card's stuck flag, not a message to any
  session, so an alert cannot wake the thing that is stuck. There is no separate push. The flag shows while the card is
  in a live status.
- One alert per stall, recorded on the revision (`stuck_alerted`). A new revision (the item moves) is a new stall.
- Owner gone means a session owner whose launcher card is missing or `dead`. A launcher in another room, and a role
  owner, cannot be asked from here, so only the 15 minutes apply to them. A notice that finds nobody at delivery (the
  held report for the orchestrator) alerts at once.
- The 2 minutes run from when the owner gave the answer, not from delivery. The retype is skipped, and its step spent,
  when the answer never reached the worker: it is still queued and typing it again would double it.
- The re-delivery rides the existing new-context carry (`contextlog.go`), so it covers a clear done by atrium's new
  context cycle, not a `/clear` typed by hand. It is once per revision and side (owner, worker), and only while the
  item is still pending: a blocked notice nobody answered, an answer the worker has not started a turn from, a
  finished notice for a finished item. The alert "then" is the stall timers above, not a second alert at the clear.
- A re-delivered answer does not change which message the item waits on. Any later prompt after delivery runs it, as in
  2a.
