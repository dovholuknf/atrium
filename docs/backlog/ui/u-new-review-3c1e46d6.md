# u-new-review-3c1e46d6. Review findings on the ui batch 3c1e46d6 (bug)

Status: done on branch claude/u-new-review-3c1e46d6, not merged. All four findings were still open on claude/main
(the review is from 2026-09-30 and none of the four files had changed) and all four are fixed. The open question about
the desktop board was answered with a test and fixed. Owned by @ui. Filed by @review 2026-09-30 after a correctness
read of claude/ui at 3c1e46d6 (the /m phone page, u-030, u-031, u-033, u-034), before @ui lands it. The brief also
named u-028 and u-039: no commit between claude/main and 3c1e46d6 carries either id, so they were not reviewed.

Scope was event handling and what reaches the network, not style. One scratch test was run to prove finding 1
(`go test -run TestScratchPhoneDecide ./internal/link/`). Nothing else was run.

Read: `m/js/*.js`, `internal/api/web.go`, the `internal/link/proxy.go` change, `hubnotify.js`, `phone-bell.js`, u-031
in `terminal.js` and `terminal-list.js`, u-033 in `terminal-links.js` and `terminal.js`, u-034 in `rooms-dash.js`. Not
read: the CSS, `index.html`, `keepalive.js`, `termfull.js`, `tcompose.js`, the headless script.

Checked and fine: `m/js/md.js` escapes every character before adding markup and keeps only http and https links, and
every other `innerHTML` in `m/js/` goes through `U.esc` or takes no model text. perms.js sets model text with
`textContent`. compose.js reaches the right room for `/v1/tasks/{id}/message`, because the hub places any request
naming a card (`placeCard`). Presence (`/_hub/presence`) counts streams per tab, so a duplicated tab does not mark
the other one gone.

## 1. MED: on a hub with two rooms, the phone cannot approve or deny anything

`internal/api/web/m/js/perms.js` `decide` calls `fetch` directly. The /m page does not load `js/rooms.js`, whose
`fetch` wrapper is what adds `X-Atrium-Room` on the board, so the phone's decide carries no room. The hub routes by
the card only for `/v1/tasks/<id>/...` paths, and a permission path is not one, so with two or more rooms attached
the decide falls through to `needsARoom`.

Proven with a scratch test through `two(t, ...)` in `internal/link` (the exact body perms.js sends):

```
aggregate row, phone:  POST /v1/rooms/alpha/permissions/alpha~p1/decide, no header -> 409 "pick a room first"
scoped row, phone:     POST /v1/permissions/p1/decide, no header                  -> 409 "pick a room first"
scoped row, board:     POST /v1/permissions/p1/decide, X-Atrium-Room: alpha        -> 200, served by alpha
```

In the all-rooms view the row's `id` is `alpha~p1`, `room` is `alpha` and `perm_id` is absent (the hub's fanout
tags `id` and adds `room`, `internal/link/fanout.go:217`), so perms.js builds the legacy `/v1/rooms/...` path with a
tagged id, which no room would understand even if it were routed. The phone shows "Not sent: that belongs to one
machine..." on every row. With one room attached (today, with sg3 and m1mini offline) it works, which is why nothing
has shown it.

Suggested fix: send the decide through `mNet.api`, which adds the scoped room, and for a tagged id split it
(`mNet.roomOf`, `mNet.bareId`) and POST `/v1/permissions/<bare>/decide` with `X-Atrium-Room: <room>`. The third line
above is that call. Could a headless mPerms case run with two rooms, so this is covered?

The desktop board sends the same all-rooms path (`settings-spine.js` `decide`, `/v1/rooms/<room>/permissions/<id>`)
and gets a header only when `roomNow()` or `writeRoom` is set. Does an all-rooms approve on the desktop board work
today? Unproven, and outside this batch.

## 2. LOW: the phone composer reads the wrong room's paste capability

`compose.js` `pasteMap` reads `/v1/harnesses` with plain `fetch`. On a hub with two rooms that is the merged list, and
the map is keyed by runner id alone, so two rooms' `claude` rows overwrite each other and the last one wins. A card
whose own room lacks `bracketed_paste` can get its line breaks sent raw, which submits early. Unproven. Could the map
be keyed by room and runner, from `mNet.api` with the card's room?

## 3. LOW: a slow list read can undo a newer event on the phone

`store.js` `loadTasks` replaces the whole map when `/v1/tasks` answers. A `task` row or a `task-removed` that arrives
while that read is in flight is overwritten by the older snapshot: a card shows its old status, or a removed card
comes back, until the next event or the 60 second resync. Unproven. Could an event that lands during a read mark the
read stale, so its answer is taken again?

## 4. LOW: reopening a card just after closing it does nothing

`card.js` `closeNow` leaves `openId` set until `finishClose`, up to 400 ms later, and `open` returns at once while
`openId` is set. A tap on the same or another card inside that window is dropped. Unproven. Could `closeNow` clear
`openId` and let `finishClose` check the sheet only?

## Also seen, not filed

The u-030 gear row makes `PUT /_hub/notify`, which sets a command the hub runs, one click away for anybody who can
open the board. Whether that endpoint needs a loopback gate is @fabric's open question f-024 from cr48. The board half
does not change the answer, but it makes the question more urgent.

## Outcome (2026-10-05)

Pictures: `docs/screens/u-new-review-3c1e46d6/`. `before-*` is perms.js from claude/main and `after-*` is this branch,
both at 390px on the real /m page with a mock two-room hub. In `before-approve-390.png` the approve is refused and the
row says "Not sent" (the mock's 404 text stands in for the hub's 409). In `after-approve-390.png` the row is answered
and the mock saw `POST /v1/permissions/p1/decide` with `X-Atrium-Room: alpha`.

1. FIXED. `perms.js` `decide` goes through `mNet.api`, takes the room from `p.room` or the tag on the id, the id from
   `perm_id` or the bare part of the id, and posts `/v1/permissions/<bare>/decide` with `X-Atrium-Room`. It no longer
   builds the `/v1/rooms/<room>/...` path at all. That path is the daemon's `RoomDecide`, which the hub does not route
   either (checked, a 409 with no header and a 200 with it), so the plain path with the header is the one call that
   works on a hub and on a single daemon. Tests: headless `mPerms` has a two-room case (tagged ids, no `perm_id`, both
   rooms answered with the right header and bare id), and the old room case now expects the plain path with the
   header. Go: `TestAPermissionDecideNeedsTheRoomHeader` in `internal/link` pins that the hub 409s a decide with no
   header on either path and serves it from the named room with one. No hub code changed.
2. FIXED. `compose.js` `pasteMap` is a map per room, read through `mNet.api` with the card's room in
   `X-Atrium-Room`, so each room's own list answers and no runner id can overwrite another room's. That is keyed by
   room and runner as the review asked, with the read itself scoped so the hub returns one room's list rather than
   the merged one. Test: headless `mCompose`, two `claude` cards in two rooms where one room cannot paste.
3. FIXED, differently from the suggestion. The review suggested marking a read stale when an event lands during it and
   reading again. `store.js` instead keeps the rows and removals the stream delivered while a read was in flight and
   lays them over the answer. It needs no second read, and a busy stream cannot keep a stale read looping. Test:
   headless `mReview`, a read answered 900ms late with a row and a removal landing in between.
4. FIXED. `card.js` `closeNow` clears `openId` at once and `finishClose` takes the id it was closing, checking the
   sheet and the close token only. Test: headless `mReview`, a reopen right after the popstate of a close, of the same
   card and of another.

Desktop all-rooms approve: it did not work. A Go test through `two(t, ...)` showed
`POST /v1/rooms/alpha/permissions/p1/decide` with no header is a 409 "pick a room first" on a hub, and the board's
fetch wrapper sets the header only when `roomNow()` or `writeRoom` is set, which the all-rooms view has neither of.
Fixed with a small change: `settings-spine.js` `decide` on a hub posts the plain path with `X-Atrium-Room` from the
card's `data-room` and keeps the `/v1/rooms/` path only off a hub. Test: headless `mReview`, both cases.

Left: the "also seen" note on `PUT /_hub/notify` stays with @fabric's f-024.

How the tests were run: `HEADLESS_ONLY=mPerms,mCompose,mReview node scripts/test-board-headless.js` with playwright
from a scratch install (it is not in this repo's node_modules), plus the other `m*` sections and `eventDriven`. All
pass. Each new case was also run against claude/main's files and fails there. The pictures come from
`REVIEW_SHOTS=<dir> REVIEW_SHOT_NAME=before|after HEADLESS_ONLY=mReview`.
