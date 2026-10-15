## Test plan

## @LETTER@. The items tab

Board code: `js/items.js`, `css/items.css`, the tab in `index.html`, and `js/backlog.js` losing the read-only list.
Hub code: `GET /_hub/items/<id>` answers `history`, and `by: board` acts as the owner (`internal/link/items.go`), from
the hub's own machine only, so the hub needs a restart for answer and cancel to work from the board.

### @LETTER@1. Grouping and the count

1. On the hub board, have one item blocked on the human, one blocked on a session, one running, one queued and one
   finished.

**Expected:** an "items" tab (in the `...` menu when the bar is full) counting only the one blocked on you. The groups
read blocked, running, queued, then finished folded shut with its count. Each row has title, state, owner, the card as
a link that opens its terminal, age in that state and the item id small and grey.

### @LETTER@2. Answer

1. Type an answer in a blocked row and press answer (or Enter). Try it empty first.

**Expected:** an empty answer is refused on the page. A typed one is typed to the worker by the room, the row says it
was sent, and it shows as running once the worker's turn starts. A refresh in the middle of typing keeps the text.

### @LETTER@3. Cancel

1. Press cancel on a running item, keep it, then press cancel again and confirm.

**Expected:** keeping it changes nothing. Confirming finishes it as cancelled and moves it to the finished group. A
finished item has no cancel.

### @LETTER@4. Detail and live refresh

1. Click a row. Then have an item block from another session with the tab open.

**Expected:** the row opens with its note, creator, times, card and a timeline of states. The new blocked item appears
and the count rises within 5 seconds, without a reload.

### Decisions where the brief was short

- The hub has no title or body for an item. The title is the title of the card that runs it, else the card id, and the
  "question" and "result" are the item's note.
- The hub refused every action on a human-owned item. The board is the human, so `by: board` is accepted for any item,
  and the write already required the hub's own machine.
