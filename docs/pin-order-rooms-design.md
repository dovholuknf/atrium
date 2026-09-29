# A pinned strip across rooms (backlog-2 item 52)

A drag in a pinned strip that holds cards from two rooms saves the order on one room. This is why, and the fix.

## Why it happens

- The board's drop (`internal/api/web/js/terminal-list.js`, `ondrop`) pins the dragged card with a `PATCH
  /v1/tasks/<id>`, then posts the whole strip as `POST /v1/tasks/pin-order {"ids": [...]}`, with every id made bare
  first.
- The pin is routed by its card (item 63, `internal/link/cardroute.go`), so it lands on the right room.
- The order is not. `pin-order` is in `notCards`, so `placeCard` does not look at it, and the ids are in the body,
  where nothing on the hub reads them. `roomFor` falls through to the board's `X-Atrium-Room` header, which on a
  write is `writeRoom`: whichever editor was open last. One room gets the list.
- That room's `SetPinOrder` (`internal/store/tasks.go`) writes `rank = i` for each id it holds and ignores the rest,
  as its comment intends. The other room's cards keep their old ranks.
- The strip sorts pinned cards by `rank` alone (`terminal-list.js` line 1584). So the other room's cards stay where
  they were, and the drag looks like it did nothing, or half of something.

## The fix: the hub passes the whole list to every room

`POST /v1/tasks/pin-order` becomes a hub-side fan-out, the way `terminal_min_cols` already is
(`internal/link/mincolssetting.go`).

- The hub reads the body, strips any `room~` tags off the ids, and posts the SAME full list to every attached room,
  in parallel, each bounded at 3 seconds. The reply waits for the slowest of those, never longer.
- Each room writes `rank = i` for the ids it holds, and `i` is the card's position in the WHOLE strip, not in that
  room's share of it. So the ranks on the two rooms interleave in exactly the order the operator dropped, and the
  merged sort reproduces it. A room that holds none of the ids writes nothing, at the cost of a few UPDATEs that
  match no row.
- It fans whether or not a header is set. A scoped view's list only holds that room's cards, so the other rooms
  match nothing, and it keeps working once item 49 puts a foreign card in a scoped strip.
- **A room that is not answering never fails or stalls the reply** (atrium-87300's guard). The answer is `200
  {"rooms": [...], "unreached": [{"room": ..., "error": ...}]}` whenever at least one room took the list, and
  `unreached` names each room that refused, errored or timed out. The board ignores the extra field today, and @ui
  may later show it. Only when NO attached room took it is the answer `502`, so the board's existing toast says the
  order did not stick. Rooms that wrote stay written: each room's write is a transaction, the fan across rooms is
  not, and the next drag rewrites the whole list anyway.
- A room that is not attached is skipped. Its cards are drawn as `remembered` and keep their old rank until it is
  back and the strip is dragged again.

Nothing changes on the room side and nothing changes on the board. The board keeps sending bare ids, the room's
handler keeps its untag safety net, and no migration is needed.

### One limit, stated rather than fixed

A card has ONE rank. A drag in sg3's scoped view writes `0..k` for sg3's cards only, which reorders them among
themselves in the ALL view and can move them past another room's cards there. Keeping a separate order per view is
a new field per view, which is item 50's question (views), not this bug's.

## Version skew

- New hub, old room: the room API is unchanged, so this works.
- Old hub, new anything: today's behaviour. The board is unchanged, so there is no new board to be old against.

## Which side

| Part | Side |
|---|---|
| intercepting `POST /v1/tasks/pin-order`, untagging, the fan, the answer | HUB-SIDE (`internal/link`, a new `pinorder.go` beside `mincolssetting.go`) |
| nothing | ROOM-SIDE |
| nothing, unless @ui wants to show `unreached` | UI (@ui) |

## Also checked

`/v1/tasks/prune` is the other entry in `notCards` and has the same shape: one room by header. Whether pruning from
the ALL view should reach every room is the worker's to check and report, not to fix under this item.

The item's second note, that pins cannot be reordered in any sort other than manual, is a board question. It goes
to clint and @ui, and is not part of this fix.

## Test plan, when built (letter FF)

- FF1: two rooms attached, pin one card on each and one more on the first. In the ALL view drag the second room's
  card to the top. Reload: it is still at the top, and the other two are in the order they were dropped in.
- FF2: the same, with the second room hung (attached, not answering). The drop answers within about 3 seconds
  with no toast, the first room's cards take their new order, and `unreached` names the second room.
- FF3: in a scoped view of one room, reorder its pins: that room's order is saved, the other room's cards are
  untouched.
- FF4 (unit, `internal/link`): the fan posts the full untagged list to every attached room and answers 200 when all
  take it. A room holding none of the ids changes nothing.
- FF5 (unit): one room answers and one never does (a handler that blocks past the bound). The reply is 200 inside
  the bound plus a margin, with the hung room in `unreached`. With every room hung or refusing, it is 502.
