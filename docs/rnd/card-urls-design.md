# Card URLs: a card has an address made of names, and every share keeps it

Status: design, 2026-09-30, @rnd. Item `docs/backlog/ui/u-new-card-urls.md`. Built by @ui (the page) and @runtime (the
routes and the guest allowlist). Designed with `docs/rnd/handle-addressed-http-design.md`, which provides the one
resolver from a name to a card. This doc builds on that one and does not repeat it. Nothing here is built.

## 1. The answer in seven lines

1. **`/alias/<alias>` opens a card**, clint's shape. It opens the card's terminal on its own, the way `#term=<id>` does
   today. An alias is unique only among one room's live cards, so a clash across rooms opens a chooser, and
   `/room/<room>/<name>` is the qualified form that never clashes (a name there is an alias or a handle).
   `/room/<room>` opens the board scoped to that room.
2. **The server serves the same page on those paths.** The page reads the path and asks the handle-addressed route,
   `GET /v1/tasks/<alias>` or `GET /v1/tasks/<name>@<room>`, for the card. No page learns a second way to resolve.
3. **A bookmark names the work, not one card id.** The name is resolved on every load. When it now opens a different
   card than it did last time, or nothing, the page says so in words instead of showing an empty terminal.
4. **`#term=<id>` and `#term=<room~id>` keep working** exactly as they do. Every link already sent stays good.
5. **Board shares over zrok and OpenZiti need nothing new.** They serve the board's own handler, so they serve the new
   paths once the hub and the room do.
6. **A lent session gets a readable address that names that card and only that card.** The guest allowlist accepts the
   path and the lookup only when the name resolves to the lent card. Any other name is the same 403 as everything else.
7. **The phone page gets `/m/room/<room>/<name>`**, and opening a card on the phone puts that address in the bar.

## 2. What is there today

| piece | where | what it does |
| --- | --- | --- |
| terminal-only window | `js/solo.js` `termOnly`, `bootTerminalOnly` | the board page with `#term=<id>` in the fragment boots as one terminal. `soloID` is the id, and the one-window-per-card claim is keyed by the bare id |
| switcher in that window | `js/switcher.js` | moving to another card rewrites the fragment with `replaceState` |
| pop-out link | `js/runners.js`, `js/solo.js` | builds `<board>#term=<id>` |
| landing a click | `js/toasts.js` `landFromURL` | reads `?land=&view=&key=` and puts the address back to `location.pathname` |
| hub pages | `internal/link/proxy.go` `asset` | serves the board's files. `/` is `index.html`, a directory is its `index.html`, anything else not a file falls through to the proxy |
| room pages | `internal/api/web.go` `webHandler` | `http.FileServer` over the board. A path that is not a file is a 404 |
| phone card view | `m/js/card.js` | opens a card with `history.pushState({mcard: id}, "")`, so the address bar never names the card |
| lending one card | `internal/daemon/overlay_guest.go` `guestHandler` | one listener per lent card. Its address is `<frontend>/#term=<id>`. An allowlist: the page and its assets, `/v1/health`, and exact `/v1/tasks/<id>`, `/attach`, `/icon` for this id. Everything else 403 |
| board over an overlay | `docs/overlays.md` | both SDKs hand back a `net.Listener` and the board is one handler on it |

Every asset and every request in `index.html`, `m/index.html` and the scripts is an absolute path (`/js/`, `/css/`,
`/vendor/`, `/v1/`). So the page served at a deeper path loads the same files. Checked, not assumed.

## 3. The shape

**`/alias/<alias>`**, which is what clint asked for (2026-09-30, via the orchestrator), and **`/room/<room>/<name>`** as
its qualified form. `<room>` is the room's name as the hub knows it, the same name `room~id` tags use.

| address | opens |
| --- | --- |
| `/alias/rnd` | the one card aliased `rnd` across every attached room, or a chooser when two rooms have one |
| `/room/claude-sg4/rnd` | the card aliased `rnd` on room `claude-sg4`, as one terminal |
| `/room/claude-sg4/rnd-director-2` | the same card by handle, for a card with no alias |
| `/room/claude-sg4` | the board, scoped to that room (what picking the room in the header does) |
| `/m/alias/rnd`, `/m/room/claude-sg4/rnd` | the phone page with that card open |
| `/#term=claude-sg4~01a0...` | unchanged |

**The clash, and why it does not break a bookmark.** Inside one room the store keeps two live cards from holding one
alias. Across rooms nothing does, so `/alias/rnd` can meet two live `rnd` cards the day a second room gets one. The
resolver answers 409 with both (the HTTP design, section 4), and the page turns that into a choice rather than an
error:

- **This browser has opened `/alias/rnd` before**, and one of the candidates is the card it opened last time (section
  5 keeps that id): it opens that one, with one line over the pane naming the other and linking to it.
- **Otherwise** it shows a chooser: each candidate as its room, its handle, what it is doing, and its qualified link
  `/room/<room>/rnd`. Picking one opens it and remembers it for this path.

So a bookmark made before the clash keeps opening the card it always opened, and a new one asks once.

**A done card.** A done card keeps answering to its alias behind any live one (`GetByAlias`: a worker that reported done
still waits at its prompt). A dead or archived card never answers. So for `/alias/rnd`:

| live `rnd` cards, all rooms | done `rnd` cards | opens |
| --- | --- | --- |
| 1 | any | the live one. If this browser last opened a done one, the notice line names it and links to it |
| 0 | 1 | the done one, with "this card is done" over the pane and its last recap line |
| 0 | 2 or more, on different rooms | the chooser, done cards marked done, newest first |
| 2 or more | any | the chooser (the clash rule above) |
| 0 | 0 | the miss: "no card called rnd", with what would have worked |

`/room/<room>/rnd` follows the same table inside one room, where the store already rules out two live holders.

**The room, not the machine, qualifies it.** The hub keys everything by room: tags, routing, the rooms list. A machine
can hold two rooms (sg4 holds `sg4-control` and `claude-sg4`), so a machine alone does not say where a card is.

**The board links with `/alias/` when it can.** A card whose alias no other attached room's live card holds is linked as
`/alias/<alias>`. A card whose alias clashes, or that has only a handle, is linked as `/room/<room>/<name>`.

**`<name>` is compared the way the resolver compares it**: case does not matter, and a leading `@` is dropped. The page
writes the alias in lower case, the way `NormalizeAlias` stores it.

## 4. Serving the page

Both servers answer `GET` on these paths with the page, and only for a browser asking for a page:

- `/alias/<alias>`, `/room/<room>` and `/room/<room>/<name>`: `index.html`
- `/m/alias/<alias>` and `/m/room/<room>/<name>`: `m/index.html`

On the hub, in `asset()`, before the fall-through to the proxy. On the room, in `webHandler`, in front of the file
server. A path deeper than these, or one with an empty part, is a 404 page naming the shapes that work. Neither
`/alias/` nor `/room/` is used by any route today (checked in `internal/link`, `internal/api`, `internal/daemon`).

The server does not resolve the name. It serves the same static page for any name, which is what it does for `/` today,
and the page asks. This keeps one place that resolves (the handle-addressed route) and one place that can say "no such
card" well (the page). The one exception is the guest listener (section 7), which is an allowlist and must check.

## 5. What the page does

`termOnly()` becomes true for a `#term=` fragment, an `/alias/<alias>` path or a `/room/<room>/<name>` path. Then, in
`bootTerminalOnly`:

1. Read the alias, or the room and the name, off the path.
2. `GET /v1/tasks/<alias>` or `GET /v1/tasks/<name>@<room>`. The answer is the card, with `X-Atrium-Card` giving its id
   as this scope names it. A 409 on `/alias/` goes to the clash rule in section 3.
3. Found: set `soloID` to that id and carry on exactly as `#term=` does from there. The claim, the one-window rule, the
   switcher and the restore loop all see an id and are unchanged.
4. The address bar keeps the readable path. It is not rewritten to `#term=`.

What it says when the name does not open what it opened last time. The page keeps, per readable path, the last id it
opened (`localStorage`, key `atrium.cardurl.<room>/<name>`, since this is a fact about this browser's bookmarks):

| lookup answers | last time | the page |
| --- | --- | --- |
| a card | none, or the same id | opens it. Nothing said |
| a card | a different id | opens it, with one line over the pane: "@rnd is a different card now, launched 11:02. the one this address opened before is done" and a link to the old one by `#term=` while that card exists |
| 404 | anything | no terminal. "no card called rnd on room claude-sg4", then the `would_work` list, each one a readable link |
| 409, on `/alias/` | anything | the clash rule in section 3: the remembered card if it is a candidate, else the chooser |
| 409, on `/room/` | anything | cannot happen (one room holds an alias for one live card). Shown as the chooser, for safety |
| room not attached | anything | "room claude-sg4 is not attached to this hub", then the attached rooms as links |

The resolver's rule decides which card: live before done, then newest, never dead (`GetByAlias`, and `matchCard` in the
HTTP design). The item suggested the same rule, and it is already the store's.

**The switcher** in a terminal-only window writes the readable path of the card it moved to (`replaceState`) when that
card has an alias or handle, and `#term=` only when it has neither.

**Links the board builds** use the readable path: the pop-out link in `runners.js` and `solo.js`, the card menu's copy
link, and `landFromURL`, which today writes `location.pathname` back and must write `/` so a landing on a readable path
does not keep it. `#term=` stays what the board builds for a card with neither an alias nor a handle, which is a card
nothing has named yet.

**Scoped board.** `/room/<room>` sets the header's room scope, the same `atrium.room` the room picker writes, and then
boots as the board. It is a page, not a new mode.

## 6. Shares

**The board over zrok or OpenZiti** (`docs/overlays.md`) is the hub's or the room's own handler on another listener.
Sections 4 and 5 make the paths work there with no change to the overlay code. A bookmark made on the share opens the
same card, because the room names are the hub's either way. The test plan covers both overlays.

**A room's own port** accepts `/room/<its own name>/<name>`, resolved by the room's own wrapper (stage R2 of the HTTP
design). A path naming another room on a room's port gets the page, and the page says "this is room claude-sg4. open
this address on the hub".

## 7. A lent session

A guest listener serves ONE card, and its allowlist must keep refusing every other one. Three additions, all exact:

1. **The page on the card's own readable path.** `GET /room/<this room>/<name>` and `GET /alias/<name>` serve
   `index.html` when `<name>` is this card's handle, or its alias as it is at the time of the request. Checked against
   the store on each request. Any other `/room/...` or `/alias/...` is 403, the same answer as every other refusal, so
   the guest page is not an oracle for which names exist on the machine.
2. **The lookup, when it names this card.** `GET /v1/tasks/<name>@<this room>` and `GET /v1/tasks/<name>` are resolved
   with the room's own resolver, and served (rewritten to `/v1/tasks/<id>`, the route already allowed) only when the
   answer is this card's id. Anything else is 403, found or not. A guest never sees a chooser: the listener is one
   room and one card, so there is nothing to clash with.
3. **The address it hands out** is `<frontend>/room/<room>/<handle>`. The HANDLE, not the alias: a handle belongs to
   one card for that card's whole life and never moves, and an alias can be changed on the board or taken by a new
   card once this one is done. A guest's link breaking because the operator renamed a card would be a surprise nobody
   asked for. The `#term=<id>` address still works on the same listener.

`showGuestPage`, the page for a guest who lost the fragment, becomes the page for a guest at `/` with neither a path nor
a fragment, and it says what the link should look like.

## 8. The phone

- `/m/alias/<alias>` and `/m/room/<room>/<name>` open the phone page with that card open. `m/js/card.js` resolves
  through the same lookup, and a clash shows the same chooser as a list.
- Opening a card on the phone pushes its readable path (the same `/alias/` or `/room/` choice the board makes) instead
  of a state with no address, so the back button still closes it and a phone bookmark or a shared link opens the card.
- A board readable path opened on a phone does what `#term=` does on a phone today. No redirect to `/m`.

## 9. The build

Sizes: S is up to half a day for one worker, M is a day. H1 and R2 are the HTTP design's stages and must land first:
this item has no resolver of its own.

| stage | owner | size | what | depends on |
| --- | --- | --- | --- | --- |
| R3 | @runtime | S | serve the page on `/alias/...`, `/room/...`, `/m/alias/...` and `/m/room/...` from the hub's `asset()` and the room's `webHandler`, and the 404 page for a bad shape. Go tests for both servers, and that neither prefix shadows an API path | nothing |
| U1 | @ui | M | `termOnly` and `bootTerminalOnly` read the path, the lookup, the table in section 5, the clash rule and chooser (section 3), the per-path last id, the switcher and the links the board builds (`/alias/` unless it clashes), `landFromURL` writing `/`, `/room/<room>` scoping the board. Headless Playwright with mocked lookups for each row of the table and for a clash with and without a remembered card | R3, H1 |
| R4 | @runtime | S | the guest allowlist's three additions (section 7), and the handed-out address by handle. Tests: this card by alias and by handle is served, another card's alias and a name that exists nowhere are both 403 with the same body, a renamed alias still opens by handle | R2 |
| U2 | @ui | S | the phone: `/m/room/...` opens a card, `card.js` pushes the readable path | R3, H1 |

R3 can start now, in parallel with the HTTP design's H1 and R1. U1 needs H1 on the hub for the lookup. R4 needs the
room's own resolver from R2, and a room deploy.

Test plan: a new section under the most recent letter in `docs/test-plan.md`. It must cover:

- a bookmark opens the same card on the hub, on a zrok board share, and on an OpenZiti board share
- a relaunched alias opens the new card with the notice line
- an unknown name lists the ones that would have worked
- an old `#term=` link still opens
- a lent session opens by its readable address and refuses another card's name with the same 403 as an unknown name
- a phone bookmark opens the card

## 10. Questions for later

clint is away and asked for progress without him (2026-09-30). The build goes ahead on these decisions, and each can
be reversed with a small change. They are also in the day's questions-for-later list.

1. **Decided: `/alias/<alias>` is the primary shape**, as clint suggested, with `/room/<room>/<name>` as the qualified
   form for a clash or a card with only a handle. His first example, `/room/sg4/sg4-control`, is read as room then
   card. A machine part (`/room/<machine>/<room>/<name>`) is left out because the hub routes by room and a machine part
   would only be checked. For later: does he want the machine in it anyway?
2. **Decided: the lent address uses the handle**, `/room/<room>/<handle>`, because a handle belongs to one card for its
   whole life and an alias can be renamed or taken by a new card. For later: would he rather hand out the alias and
   accept that a rename breaks the guest's link?
