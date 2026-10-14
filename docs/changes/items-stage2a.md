## Test plan

## @LETTER@. Items, stage 2a

The hub owns items (`hubstore` migration `0018_item`) and each room keeps a mirror and an outbox (`store` migration
`0098_item_mirror`). Owners get `atrium_item`. Stage 1 is `docs/changes/items-stage1.md`. Stuck alerts and re-delivery
after a context clear are part 2b and are not here. The design is `docs/rnd/atrium-items.md` and
`docs/rnd/atrium-items-decisions.md`.

Which code is which, because it decides the deploy:

- **Hub code:** `internal/hubstore` (item table, upsert, owner resolution), `internal/link` (`/_hub/items`, the
  `atrium_item` hub tool, the hub-ask whitelist) and `internal/cli/atrium_run.go`.
- **Room code:** `internal/store` (mirror, outbox), `internal/daemon` (`item_hub.go`, finish, launch, hooks),
  `internal/api` (`/v1/items/{id}/{act}`) and the stdio tool and `atrium launch --item-owner` in `internal/cli`.
- **Board code:** the read-only "items for you" block in the backlog tab (`js/backlog.js`, `index.html`).

### @LETTER@1. Owner resolution

1. From an orchestrator, `atrium_launch_worker` with `item: true` and no `item_owner`. Then with `item_owner` set to
   `merger`, then `human`, then `tester`.

**Expected:** the first is owned by the orchestrator, the second by whoever holds the merger role, the third by the
human. `tester` is refused before any card or worktree exists, with "the tester has several holders". A launch from the
operator's own `atrium launch --item` with no `--item-owner` is owned by the human.

### @LETTER@2. Blocked is blocked, and one notice

1. Have the item worker call `atrium_blocked` twice with different words.

**Expected:** the item is `blocked` at one new revision. The owner has ONE notice from `atrium` that names the item,
the ask and `atrium_item unblock <id>`. The worker was told nothing. Nothing is sent to the launcher as a report.

### @LETTER@3. Finished is terminal

1. Have it call `atrium_done` with a sha, again with another sha, then `atrium_blocked`.

**Expected:** one `finished` notice. The second done and the late blocked change nothing, and the item stays finished.

### @LETTER@4. Unblock runs the item only on the worker's turn

1. As the owner, `atrium_item unblock <id> "use KEY-9"` on the blocked item.
2. Read `atrium_item get <id>`. Let the worker's turn start.

**Expected:** the worker is sent the answer as `atrium` and the words do not name the owner. Until its turn starts the
item stays `blocked`, also once the message is delivered. When the turn starts (a prompt, or the Stop hook carrying
the answer) it is `running` at a higher revision. A human typing into the worker's terminal starts the turn too.
Unblocking an item that is not blocked is a 409.

### @LETTER@5. Cancel

1. `atrium_item cancel <id>` on a running item, then again.

**Expected:** the item is `finished` with the note "cancelled by owner", the worker is asked to leave, the owner gets
no notice, and the second cancel is a 409.

### @LETTER@6. A hub outage loses nothing

1. Stop the hub. Launch an item and have it block. Start the hub.

**Expected:** the room records both changes locally. Once the hub is back the outbox is pushed oldest first
(`running` r1, `blocked` r2), a replay of any of them is a no-op on the hub, and `atrium_item list` shows the item.

### @LETTER@7. A role owner and a human owner

1. Launch an item owned by `merger` while a merger is running, and have it block. Repeat with owner `human`.

**Expected:** the merger's card gets the notice. With nobody holding the role the end is held for the orchestrator like
any report that found nobody. The human-owned item writes no notice to any session, and the board's backlog tab shows
it under "items for you", with no button or field to unblock it.

### Decisions where the spec was ambiguous

- The room reaches the hub for items through the existing `hub` relay op (`HubAsker`), not a new kind. The outbox is
  `item_outbox`, keyed `(item_id, revision, state)`, written in the same transaction as the mirror.
- The notice is queued by the room, keyed `item/revision/state`, and not by the hub. A role owner is resolved at
  delivery by asking the hub for the holder. Unresolved falls back to the orchestrator's held report.
- The owner on an unblock or cancel is a claim (`by`). A role owner is checked by handle against the holder's card.
- An operator launch with no creating session defaults to `human`.
- The owner travels as the tag `atrium:item-owner:<spec>`, which avoids a new field through every relay.
- Finished is terminal. The same state again is no change, so a replay is one revision and one notice.
  `finished` for an item whose worker ended with no result is 2b.
- Cancel is room-side. A session owner is stored as `handle@room`, which the hub qualifies.
- A blocked item's ask lives on the card and in the notice, not in the item's `note`, because the note changing
  would be a revision and the same state with new words is one notice.
