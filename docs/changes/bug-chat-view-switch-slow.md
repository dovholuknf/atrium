Docs: none, a fix to how fast an existing tab switches

# Chat tab switch speed

On a live board of 784 cards, switching to Chat took about 1.8 s (1.4 to 3.0 s) and Terminals about 130 ms. Every switch
built a new `/m/?chat=1` iframe, so the whole phone page booted again (about 65 requests and 14 MB). Inside it the card
was opened only after the full `GET /v1/tasks` (6.4 MB) finished.

## What changed

- `js/chat.js` keeps one iframe per card, up to 4, least recently shown dropped first. A frame is hidden, not removed,
  when the tab is left or another card is picked. A card's frame goes when the card leaves the task list, and every frame
  goes when the room scope changes, since a frame stamps `X-Atrium-Room` from `localStorage`.
- A hidden frame holds no event stream. The board posts `{ atriumChatPark }` to the frame and `m/js/store.js` closes its
  stream, and opens it again when the frame is shown, so the browser's six connections to the host are not spent on
  frames nobody sees.
- `m/js/card.js`: when the address names a card (`#term=<id>`) and the store does not hold it, one
  `GET /v1/tasks/<id>` seeds the store and the card opens, while the full list still loads after.

## Measurements

Mocked board, 800 cards, click to replies drawn, in ms, by `CHAT_MEASURE=800` on the `chatMeasure` unit. The third
column holds the phone page's list read back by 1.5 s (`CHAT_MEASURE_LIST_MS=1500`), which is what 6 MB costs on a real
link. The mock answers locally, so the first two columns are the floor.

| Switch | Before | After | Before, list at 1.5 s | After, list at 1.5 s |
| --- | --- | --- | --- | --- |
| First visit to a card (3 runs) | 125, 98, 83 | 103, 84, 83 | 1624, 1594, 1600 | 108, 97, 82 |
| Back to a card already visited (2 runs) | 85, 150 | 14, 19 | 1601, 1633 | 11, 16 |
| Terminals, then Chat again (3 runs) | 239, 245, 259 | 195, 175, 169 | 1773, 1754, 1779 | 189, 190, 168 |

## Test plan

## @LETTER@. Chat tab switches without reloading

### @LETTER@1. A frame is kept per card

1. Open Chat and pick a card, then open Terminals and Chat again.
2. Pick a second card, then the first again.

**Expected:** the conversation is there at once each time, with no reload of the page inside the pane. The title and
the selected row follow the card shown.

### @LETTER@2. At most four are kept

1. Pick five different cards in Chat, then the first again.

**Expected:** the first card's conversation loads afresh and the others stay as they were.

### @LETTER@3. The first visit does not wait for the list

1. On a board with hundreds of cards, open Chat and pick a card never opened this session.

**Expected:** its replies draw before the full card list has arrived.
