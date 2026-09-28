# A card id carries its room end to end

Backlog-2 item 63, stage 2. Stage 1 (commit e7d15a1, HUB-SIDE) made the hub route every request that names a card to
the room holding it. This note lists every place that still strips or ignores a card's room, and proposes the rule
that keeps it: outside a room, a card id is `room~id`, and nothing that names a card is routed by anything else.

## What happened

With claude-sg4, sg4-wsl and m1mini attached, clint started the done card tlsuv/fix-ci (`01a0e9aa`, on claude-sg4)
and got "no card 01a0e9aa... to start onto: sql: no rows in result set". Then a drag of the same card into a group
answered "that did not stick: sql: no rows in result set". Both requests reached a room that does not hold the card.

The cause is one board-global. `window.fetch` in `internal/api/web/js/rooms.js` puts `X-Atrium-Room: writeRoom` on
every write whose url is not `/v1/tasks/<id>/<verb>`. `writeRoom` is left over from the last per-machine editor that
was open, and nothing clears it. The start is `POST /v1/launch` with the card in the body. The drag is
`PATCH /v1/tasks/<id>`, with no verb after the id. Both got the stale header. The hub read the header before the
card, and never read a body at all.

## Stage 1, as built

`internal/link/cardroute.go`, called from `ServeHTTP` ahead of `startsNothing`:

- a card in the path (`/v1/tasks/<id>...`, except `prune` and `pin-order`) or in a launch's `task_id` names the room
- a `room~id` tag names it outright, and the tag is stripped before the room sees it, in the body as in the path
- a plain id is looked up across the attached rooms by `roomHolding` when two or more are attached, cached for
  two minutes, since a card never moves and the cache rechecks that its room is still attached
- a card no attached room holds is a 404 from the hub: `card X was not found on room a, room b`, plus the offline
  room the hub last saw it on, if any
- `roomFor` puts that answer first, then a tag in the path, then the header, then the query

## Every place that strips or ignores the room today

Routing, where a wrong room is a wrong answer:

1. **The board's `writeRoom` header.** `rooms.js`, `window.fetch`. Set on any write that does not match
   `/v1/tasks/<id>/<verb>`, so `PATCH` and `DELETE /v1/tasks/<id>` and a launch onto a card all carry it. The hub
   now overrides it for anything naming a card, but the board still sends a room it has no reason to believe.
2. **The board strips the tag off a launch's `task_id`.** `card-menu.js:274` (resume, unshelve's launch),
   `fixtures.js:1233` (launch a runner onto a card), `fixtures.js:1522` (the launch form's target). The comment says
   the room keys by the bare id, which is true, but the hub is the place to strip it. Stripped on the board, the one
   fact that says where the card lives is thrown away before the request leaves the page.
3. **A launch's answer comes back bare.** `retagCard` retags only when the tag arrived in the PATH (`taggedKey`). A
   launch onto `beta~x` answers `{"id":"x"}`, so the board then holds a bare id for a card it listed tagged, and every
   url it builds from that answer relies on the stage 1 lookup.
4. **A scoped view hands out bare ids.** With a header or `atrium_room`, the hub is a pipe: lists, events and single
   cards come back with the room's own ids. A terminal or popped-out window opened in a scoped view keeps its bare id
   after the board switches to ALL. Stage 1 routes those by lookup. Nothing is wrong, but the id alone no longer says
   where the card is.
5. **The event stream tags only in the aggregate view.** `serveEvents` tags when `tag && p.hub.Only() == ""`. Same
   as item 4, for the same reason.
6. **One room attached skips the lookup.** `placeCard` does nothing with fewer than two rooms, so a card that
   belongs to a room that is offline goes to the one live room and gets that room's own error. That room's error is
   item 7.
7. **The room's own not-found is a bare `sql: no rows`.** `internal/daemon/launch.go:757` wraps the store error, and
   the task `PATCH` answers the store error as given. A request that does reach the wrong room, through a hub older
   than stage 1 or a room reached directly, says "no rows" and not "this card is not on room R".

Not routing, and correct as they are:

8. **The hub strips the tag on the way into a room.** `untag` for the path, `launchCardIn` for the launch body. The
   room minted the bare id and knows nothing of rooms. This is the boundary, and it stays.
9. **The room's launch strips a tag too.** `launch.go:664`, a safety net under the hub's stripping. Harmless.
10. **Board-local keys use `bareId`.** `notify.js`, `newcard.js`, `seen.js`, the popped-out window names in
    `solo.js` and `switcher.js`, the `oneAtATime` keys. These compare cards, they do not route them, and a card id is
    unique across rooms, so bare is a correct key.
11. **`spawned_by` in a launch body** names the launcher's card, which can be on another room. It is stored as a
    reference and never routed on. `atrium_say name@room` (item 58) is the place cross-room addressing belongs.
12. **The control MCP** scopes its own calls with the caller's `X-Atrium-Room`, and names cards in paths. Stage 1
    covers it: a card named there goes to its room whatever the header says.

## The rule

Outside a room, a card id is `room~id` whenever it is handed out and whenever it is sent back. The hub is the only
thing that strips a tag, and only on the hop into the room. Nothing that names a card is routed by a header.

## What to build now, if review agrees

Small, and all but B4 on the hub's side (the hub serves the board):

- **B1. The board stops putting `writeRoom` on a request that names a card.** `rooms.js`: a write names a card if its
  url is `/v1/tasks/<id>` or deeper, other than `prune` and `pin-order`, or it is a launch with a `task_id`. Those
  get the explicit scope or nothing. `writeRoom` is left for writes that name no card, which is what it is for.
- **B2. The board sends `task_id` as it holds it.** Drop `bareId` at the three launch sites. The hub strips the tag
  (stage 1). Against a hub older than stage 1 the room's safety net strips it and the header routes, as today.
- **B3. The hub retags a launch's answer** when the body's `task_id` was tagged, the same as a tagged path, so the
  card the board gets back names its room.
- **B4. The room's not-found names the card and the room.** `launch.go:757` and the task `PATCH` answer
  `card X is not on room R` for a missing card, not the store's `sql: no rows`. ROOM RESTART, so it lands with the
  next room deploy and nothing waits on it.

Left for later, and not needed for item 63:

- **Tag in the scoped view too** (items 4 and 5), so an id never goes bare anywhere outside a room. It changes what
  a scoped board and every scoped control MCP caller reads, which `TestAHeaderScopesToOneRoom` pins today as a pipe.
  Worth doing when a second thing needs it.
- **Mint ids with their room.** A room that stored `room~id` would carry its room even through a hub-less path. It
  touches every store row and every id already out there, for a problem the hub now solves at the boundary.
