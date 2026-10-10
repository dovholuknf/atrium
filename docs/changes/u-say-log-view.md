## Test plan

## @LETTER@. A card's details list its chatter

### @LETTER@1. The list and a say's full text

1. Have two sessions say to each other, one of them with a long message.
2. Open `details` on one of them, on the terminals tab or from the board peek.

**Expected:** a "chatter" section lists the says to and from the card, newest first. Each row has `to` or `from`, the
other party, how long ago, and the delivery state (`delivered by terminal`, `queued` and so on). Clicking a row opens
its full text. A say longer than 16000 bytes ends with a note of how much was kept. A card with no says shows no
section.

### @LETTER@2. The other party links to its card

1. Click the name of a party whose card is on the board.
2. Look at a row whose card is gone.

**Expected:** the first opens that card (attaches it when it has a terminal). The second is plain text, not a link. A
card on another room works the same through the hub, since the hub places `/v1/tasks/<id>/says` by the card's id like
every other card call.

### @LETTER@3. More pages back

1. Give a card more than 20 says and open its details.
2. Click `more`.

**Expected:** the next 20 older rows are added under the first, and `more` is gone once the oldest is shown.
`GET /v1/tasks/<id>/says?before=<id of the last row read>` is the same call, and it answers `more: true` while
another page is behind it. `node scripts/test-board-headless.js chatter bootClean` checks the section, and
`go test -tags integration ./internal/store/ ./internal/daemon/` the paging and the stored text.

Pictures: `img/say-log-view-before.png` and `img/say-log-view-after.png`.
