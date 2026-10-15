## Test plan

## @LETTER@. Items, stage 3: pools and LEARNINGS.md

Pools and the pool's memory, on top of `docs/changes/items-stage2a.md` and `items-stage2b.md`. The design is
`docs/rnd/atrium-items-decisions.md` sections 7 and 8.

Which code is which, because it decides the deploy:

- **Hub code:** `internal/link` (`items.go` `POST /_hub/items` with `do: submit`, `items_mcp.go` the `submit` action,
  `hubask.go` keeping `room` on a submit, `ctl_end.go` the `learned` field). The hub needs a restart.
- **Room code:** `internal/store` (migration `0102_item_pool`, `item.go`), `internal/daemon` (`item_pool.go`,
  `learnings.go`, `item_hub.go`, `finish.go`, `reaper.go`), `internal/api` (`POST /v1/pools/{pool}/items`). The room
  needs a restart to pick up the migration.
- **Board code:** none.

How it works:

- A pool is the resident cards tagged `atrium:pool:<name>` (`atrium_launch` with `resident: true` and that tag).
- The queue lives on the pool's room, since a pool is cards on one room. A queued item is not on the hub: the hub learns
  of it when a card takes it, as its first push (`running`, revision 2).
- A card is busy from the moment it is typed an item until the item is `finished`. A `blocked` item counts as busy. A
  new item goes to the first idle card, and waits when there is none, in submission order. The brief is typed as
  `atrium`, after the turn that ended the last item, never mid-turn.
- `learned` on `atrium_done` is one paragraph, up to 1500 bytes. Atrium appends it to `LEARNINGS.md` in the card's
  directory as `## <item id> <time>` and a paragraph, and commits nothing. Only the human edits or prunes.
- The cap is 50 entries or 20 KB. The append that reaches it, and every one after, raises an alert on the card (an
  event). An entry that would pass the cap is not written, and nothing already there is dropped.
- The fallback from the decisions (a compaction item for the pool when the cap is hit) is NOT built. It is for when the
  alert proves to be a blocker.

### @LETTER@1. One item at a time per card

1. Launch one resident card with the tag `atrium:pool:reviewer`. As an orchestrator, `atrium_item submit` two items to
   `pool: reviewer`.
2. Read the card's terminal, then have the card end the first with `atrium_done`.

**Expected:** only the first brief is typed, as `atrium`, and the second item stays `queued`. After the done, the
second is typed when the turn ends, and `atrium_item list` shows the first `finished` and the second `running`.

### @LETTER@2. More cards, more throughput

1. Add a second card to the pool and submit three more items.

**Expected:** each idle card takes one, in order, and the third waits for whichever card finishes first.

### @LETTER@3. A blocked item holds its card

1. Have the card block an item with `atrium_blocked` while another item waits.
2. As the owner, `atrium_item unblock` it.

**Expected:** the waiting item is not typed to a card with a blocked item. Once the item is unblocked and finished the
next one is typed. Submitting to a pool with no live card is refused with the tag to launch it with.

### @LETTER@4. The owner is the submitter

1. Submit with no `owner` from a session, then with `owner: human`.

**Expected:** the first item's blocked and finished notices go to the submitting session and not to the pool card's
launcher. The human-owned item writes no notice to any session. A submitter on another room than the pool's is refused
unless the owner is `human` or a role.

### @LETTER@5. Cancel

1. Cancel a queued item, then a running one.

**Expected:** the queued one is `finished` with "cancelled by owner" and nothing is pushed to the hub for it. The
running one is `finished`, the pool card is told to stop work on it and is NOT asked to leave, and the next item is fed.

### @LETTER@6. LEARNINGS.md

1. End an item with `atrium_done` carrying `learned`. Send the same done again.
2. Submit another item.

**Expected:** `LEARNINGS.md` in the card's directory has one entry, headed by the item id, and the replay adds none. The
next item's brief ends "Read LEARNINGS.md (1 entries) before you start." A card outside a pool leaves no file.

### @LETTER@7. The cap

1. Fill `LEARNINGS.md` to 49 entries by hand. End an item with `learned`, then another.

**Expected:** the 50th is written, and the card shows an alert that the cap was reached. The next is NOT written, and a
second alert says its learning was lost. The file is otherwise untouched. The same with a file within one entry of 20 KB.

### Decisions where the spec was ambiguous

- The pool queue is on the room, not the hub, because a pool lives on one room. The hub's item table has no `queued`
  rows from a pool, and `atrium_item list` does not show a waiting item.
- A pool card is any card carrying the pool tag that is not dead or parked. It is expected to be resident.
- Cancelling a running pool item does not ask the card to leave, since the card serves the next item.
- The pointer to LEARNINGS.md rides on each brief that is typed, instead of a SessionStart hook after a clear.
