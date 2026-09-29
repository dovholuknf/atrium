## Changelog

- **A pinned strip across rooms keeps its order.** See `docs/backlog-2.md` item 52 and
  `docs/pin-order-rooms-design.md`.

  - `POST /v1/tasks/pin-order` is a hub-side fan-out (`internal/link/pinorder.go`). The hub strips any `room~` tag
    off each id and posts the same full list to every attached room in parallel, whether or not `X-Atrium-Room` is
    set. Each room writes `rank = i` for the ids it holds, `i` being the card's place in the whole strip, so the ranks
    on two rooms interleave in the order the operator dropped.
  - A room that is not answering never fails or stalls the reply. Each post is bounded at 3 seconds. The answer is
    `200 {"rooms": [...], "unreached": [{"room", "error"}]}` while at least one room took the list, and `502` only
    when none did. The board ignores the extra field today.
  - `SetPinOrder` writes pinned rows only (`WHERE id = ? AND pinned = 1`). It has written `rank` as well as
    `pin_order` for a while, so a card unpinned in another tab between the drag and the drop had its ordinary rank
    overwritten and moved in its group. The dragged card is pinned before the order is posted, so it is never
    dropped.
  - Not changed: the board, the room API, the schema. `/v1/tasks/prune` has the same shape (one room by header) and
    is left alone here, to be looked at under its own item.
  - Stated limit: a card has one rank, so a drag in a scoped view reorders its cards among themselves in the ALL
    view too. A separate order per view is item 50's question.

## Test plan

## @LETTER@. A pinned strip across rooms

### @LETTER@1. Two rooms, one strip

Two rooms attached, pin one card on each and one more on the first. In the ALL view drag the second room's card to
the top, then reload.

**Expected:** it is still at the top, and the other two are in the order they were dropped in.

### @LETTER@2. One room hung

The same, with the second room hung (attached, not answering).

**Expected:** the drop answers within about 3 seconds with no toast, the first room's cards take their new order, and
`unreached` names the second room.

### @LETTER@3. A scoped view

In a scoped view of one room, reorder its pins.

**Expected:** that room's order is saved, the other room's cards are untouched.

### @LETTER@4. The fan reaches every room (unit)

`internal/link`, `TestThePinOrderReachesEveryRoomWhole`.

**Expected:** the fan posts the full untagged list to every attached room, with or without a room header, and answers
200.

### @LETTER@5. A hung room does not stall it (unit)

`TestAHungRoomDoesNotStallThePinOrder`, `TestPinOrderIs502WhenNoRoomTakesIt`.

**Expected:** one room answers and one never does: 200 inside the bound plus a margin, the hung room in `unreached`.
Every room refusing: 502.

### @LETTER@6. An unpinned card keeps its rank (unit)

`internal/store`, `TestAnUnpinnedCardKeepsItsRankWhenTheOrderLands`. Pin two cards, unpin one, then `SetPinOrder`
with both.

**Expected:** the pinned one takes its new rank and the unpinned one keeps the rank it had.
