Stage L7 of `docs/rnd/item-lifecycle-design.md`: blocked items, and the items on the phone.

- **The question on the row and in the detail.** A blocked item shows its question on the list row and above the timeline,
  with up to three suggested answers as buttons (the first, the worker's own recommendation, in orange) and a text box.
  A button answer is typed to the worker as `atrium`, as items stage 2a does, and the item leaves `blocked` when the
  worker's turn starts, so the notice says it was sent and does not claim the item is running.
- **Approve and send back** are in the same box for a design waiting on the human. They post to `POST /_hub/work/<backlog
  id>` (L2) as `board`. Approve names the design sha it was shown, and send back needs a note, which is typed to the
  designer. `js/items-data.js` reads `/_hub/work` beside `/_hub/items` and lays the engine's steps over a row with the same
  backlog id. An older hub with no such route keeps the one step it had.
- **The Items tab** sits with the daily work in the top nav, after chat, so it is in the bar and not the overflow menu. It
  shows on a board with no hub when the page is loaded with `?itemsmock=1`.
- **The phone.** `/m/` has an `items` button in the header (hidden until a hub answers, with the count blocked on you). It
  opens a sheet with the list in one column, the same rows, the question and its answers as full width buttons, and an
  item's page with the question above the timeline. Back is history. It is live by the hub's `item` event.
- The fixtures' priorities are the backlog's own words (`high`, `medium`, `low`), not `normal`.

The built views: `docs/rnd/img/item-lifecycle/blocked-built-1400.png` (the question above the timeline),
`phone-list-built.png`, `phone-blocked-built.png` and `phone-design-built.png`.

## Test plan

## @LETTER@. Blocked items and the phone

### @LETTER@1. A worker's question

1. Open the board with `?itemsmock=1` and the Items tab.
2. On `f-ptyhost-reconnect`, click the first suggested answer on the row.

**Expected:** the row stops being blocked and the note says the answer was sent and the item shows as running once the
worker starts its turn. Opening the item shows the same three answers above the timeline, the first orange.

### @LETTER@2. A design waiting for approval

1. On `rd-item-lifecycle`, click `send back` and then `send` with the box empty.
2. Type a note and send again. Reload, and click `approve`.

**Expected:** the empty note is refused in words and nothing is sent. The note sends the item back to `designed`. Approve
makes `approved` done and `building` active, and the note names the design sha.

### @LETTER@3. The phone

1. Open `/m/?itemsmock=1` at a phone width and tap `items`.
2. Tap a blocked row's answer, then the row's title, then back.

**Expected:** one column of rows with the bar, the step line and one line of tokens, dollars and age. The blocked rows come
first, oldest question first, with the answers as full width buttons. The page shows the question above the timeline.
