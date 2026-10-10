# say-log-view notes (u-say-log-view)

## Notes

- The text lives in a new `say.text` column (migration `0093_say_text`, last in the slice). It is cut at
  `store.SayTextLen`, 16000 bytes, on a character boundary. `chars` is still the whole length. Rows from before the
  migration have no text and show their 200 character preview. The sweep that keeps 2000 rows for 30 days bounds the
  table at about 32 MB.
- Only the room's own card is read, so a say to or from a card on another room appears on both rooms' cards only if
  each room recorded it. Nothing was added to the hub.

## Phase 2 landed (u-say-log-view-all)

- Room: `GET /v1/says`, the card clause dropped from the same query (`SaysWhere`). Filters `from`, `to`, `agent` (either
  end), `state` and `since`, each name a wire name or a card id. Twenty a page, `before` and `more` as before.
- Hub (`internal/link/says.go`): asks every attached room for one page from its own position, merges by `sent_at` and
  cuts 20. The `cursor` it answers is base64 of `{room: last id shown}`, `-` for a room with nothing further back. A
  room that did not answer keeps its position and is named in `missing`. Rows gain `room` and `room~id` on `id`,
  `from_task` and `to_task`. `room` narrows the rooms asked, and a `room~id` in `from`, `to` or `agent` asks only that
  room and arrives bare.
- Board: the chatter tab (`js/chatter.js`), read on open and on `more` and not live.
- Retention is unchanged: 30 days and 2000 rows per room, so the view is that much history and no more. A hub with
  several rooms shows up to 2000 per room.
- Not done: a way to follow one conversation (a pair of cards) across both directions. The agent filter with sent or
  received is the closest.

## Phase 2, the all-cards view, would have needed

- An endpoint that is not scoped to one card: `GET /v1/says` with the same `before` cursor and a `from`, `to` and
  `state` filter. The store query is `SaysBefore` without the card clause.
- The hub has to fan that out to every attached room and merge by `sent_at` (ids are not comparable across rooms),
  paging with a cursor per room. A `room~id` tag on `from_task` and `to_task` so the board can link them.
- A place to show it: a tab or a board overlay, with filters by sender, recipient and state, and a way to follow one
  conversation (a pair of cards) across both directions.
- A decision on retention, since the current 30 days and 2000 rows would be the whole history of every card together.
