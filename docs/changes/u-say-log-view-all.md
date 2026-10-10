## Test plan

## @LETTER@. The chatter tab lists every say from every room

### @LETTER@1. The list, its text and its links

1. Have sessions on two rooms say to each other, one of them with a long message.
2. Open the `chatter` tab from the `...` menu, where it starts. `arrange tabs...` can put it in the bar, and reset puts
   it back in the menu.

**Expected:** the says of every room, newest first, each row with the time, the room, from, to, the delivery state and
a one line preview. Clicking the preview opens the full text, and a say longer than 16000 bytes ends with a note of how
much was kept. Both names open their cards when the board has them and are plain text when it does not. A room that
did not answer is named above the list and the rest still show. The tab reads once on open and does not change while
it is open: a new say shows the next time it is opened.

### @LETTER@2. Filters and more

1. Type an agent's name in `agent`, then choose `sent`, `received` and back to `sent or received`.
2. Choose a room, then a time window.
3. Without a filter, give the rooms more than 20 says between them and press `more`.

**Expected:** each change is one new read from the top. `sent` lists only what that agent said, `received` only what
it was told, and the empty choice both. The room and `when` choices narrow the same way. `more` adds the next 20
older rows across all rooms, in time order with none repeated or missing, and goes once the oldest is shown. A room
that has fewer says than the others runs out first and is not asked again.

### @LETTER@3. The calls behind it

1. `GET /v1/says` on a room: `before=<id of the last row read>`, `from`, `to`, `agent` (each a wire name or card id),
   `state` and `since` (RFC3339) narrow it, and `more` says whether another page is behind.
2. The same call on the hub merges every attached room by `sent_at`, tags each row with `room`, writes `from_task`
   and `to_task` as `room~id`, and answers a `cursor` to send back as `before`. A quiet or detached room is named in
   `missing`. Retention is unchanged, each room keeping 30 days and 2000 says.

**Expected:** `go test -tags integration ./internal/store/ ./internal/daemon/ ./internal/link/` passes, and
`node scripts/test-board-headless.js chatterTab bootClean` passes.

Pictures: `img/say-log-all-before.png` (the `...` menu without it), `img/say-log-all-menu.png` (with it) and
`img/say-log-all-after.png` (the tab).
