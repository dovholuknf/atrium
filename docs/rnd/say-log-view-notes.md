# say-log-view notes (u-say-log-view)

## Notes

- The text lives in a new `say.text` column (migration `0093_say_text`, last in the slice). It is cut at
  `store.SayTextLen`, 16000 bytes, on a character boundary. `chars` is still the whole length. Rows from before the
  migration have no text and show their 200 character preview. The sweep that keeps 2000 rows for 30 days bounds the
  table at about 32 MB.
- Only the room's own card is read, so a say to or from a card on another room appears on both rooms' cards only if
  each room recorded it. Nothing was added to the hub.

## Phase 2, the all-cards view, would need

- An endpoint that is not scoped to one card: `GET /v1/says` with the same `before` cursor and a `from`, `to` and
  `state` filter. The store query is `SaysBefore` without the card clause.
- The hub has to fan that out to every attached room and merge by `sent_at` (ids are not comparable across rooms),
  paging with a cursor per room. A `room~id` tag on `from_task` and `to_task` so the board can link them.
- A place to show it: a tab or a board overlay, with filters by sender, recipient and state, and a way to follow one
  conversation (a pair of cards) across both directions.
- A decision on retention, since the current 30 days and 2000 rows would be the whole history of every card together.
