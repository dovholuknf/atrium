# Board deep links

Today the board's view lives in memory, in `localStorage` (`atrium.view`, `atrium.term`, `atrium.reposView`) and in
history state objects that carry no URL. Clicking a tab, a repos sub-view or a change request leaves the address at
`/`. So a reload restores only what `localStorage` kept, a link cannot be shared, and a board test reaches a change
request only by a chain of clicks. `changeReq` is 259 lines over 16 pages and fails `page.click` at 30 s on Windows in
every run.

This design puts the view and the selection in the URL, in the scheme that already exists, so that loading a URL lands
there and a test can be "load this URL, assert".

Backlog item `f-board-deep-links`. Status: designed, not built.

## What exists

- `board.js` `navState` / `navPush` / the `popstate` listener: history entries `{atrium, view, card, item}` pushed
  with the SAME URL. Its header says the address bar is not touched on purpose, because `#term=<id>` already means
  "this window is a pop-out of that card" and a view in the hash would collide with it.
- `switcher.js` `soloSwitch`: a pop-out moves between cards with `replaceState`, so back does not walk the window
  through cards it used to show.
- `#term=<id>` and the readable card paths (`/alias/<name>`, `/room/<room>/<card>`): a pop-out resolves its card from
  them (`termOnly`, `cardUrlIsCard`).
- `toasts.js` `landFromURL`: a notification click opens `/?land=<card>&view=<view>&key=<request>`, lands once, and
  strips the query.
- `?crmock=1` (`changereq-core.js`): the change-request mock, on for as long as the query holds it. `mockOn` reads
  `location.search` on every call.
- `?why=1`: the phone redirect's diagnostic line.
- `restoreWhereYouWere` (`solo.js`): the remembered view and card, put back after the roll call.

## Decisions

### 1. Query, not hash

The route is in the query string of `/`. The hash stays `#term=` and nothing else, and a card path stays a pop-out.
That is the collision `board.js` was avoiding, and a query avoids it without giving up the address bar. The query is
already where the board reads one-load switches (`land`, `crmock`, `why`), the server serves `index.html` for `/`
whatever the query, and the phone redirect in `index.html` already carries the query along to `/m/`.

The route's parameters, each optional, all others left alone:

| param | value | meaning |
|---|---|---|
| `tab` | a `VIEWS` name, as spelled there (`board`, `terms`, `hubrepos`, `items`, ...) | the board tab |
| `card` | a card id | with `tab=terms`, the card attached in the pane |
| `item` | an item id | with `tab=items`, the item page open |
| `repos` | `shelf`, `ledger`, `feed`, `requests`, `branches` | with `tab=hubrepos`, the sub-view |
| `cr` | a change request id | with `repos=requests`, the request selected |
| `crs` | `open` or `closed` | with `repos=requests`, the list tab |

`tab`, not `view`, because `view` already belongs to the notification landing (`landFromURL` lands on any `?view=`)
and one word must not mean two things. The names are the code's own names, with no alias table, so a test, a log line
and the URL say the same word.

The order the board writes them in is the order above, so one state is one string. A value the board does not know is
dropped from the route and named in the readiness mark (decision 3).

Examples a test loads:

```
/?tab=hubrepos&repos=requests&cr=cr-0003&crs=closed&crmock=1
/?tab=terms&card=01a13a76-d671-7d26-b637-51c6d7f8bff7
/?tab=items&item=f-board-deep-links
```

### 2. A move somebody makes pushes, a choice the board makes replaces

The rule `navPush` already follows, written down and applied to the new parameters:

- **Push** for a move a person makes and would press back to undo: a tab, a card attached, an item opened, a repos
  sub-view, a change request selected.
- **Replace** for what the board chose for them or what only narrows a list: the first change request picked when none
  was asked for, the `open` or `closed` list tab, an unknown value dropped, the route written after the remembered view
  was restored, and every move inside a pop-out (`soloSwitch`, unchanged).
- **Nothing** when the new state equals the current one, as `navPush` does today, extended to compare every route
  parameter. Without it a repaint that re-runs `switchView` stacks entries.

`navPush` gains a URL: `history.pushState(state, "", routeURL(state))`. `routeURL` builds the query from the state and
keeps every parameter the route does not own (`crmock`, `why`, anything added later) exactly as it found it. The
`popstate` listener applies the state as today, plus the repos sub-view and the change request, and writes nothing.

`board.js`'s header comment is rewritten: the address bar IS touched now, in the query only, and the reason it was not
(the hash) is the reason the route is not in the hash.

### 3. Data that has not arrived: a pending selection, and a mark when the route is done

Loading a route applies it in two steps.

**Before the cover comes off** (`doneSwitching` in `boot.js`): everything that needs no data. The tab, the repos
sub-view, the `crs` tab. The page is first shown already on the right tab, never on the remembered one and then the
linked one.

**When the data arrives**: the selection. `cr`, `item` and `card` are held as WANTED, not as selected:

- The change request list does not auto-pick its first request while a wanted id is pending. The detail pane says
  `opening <id>` in the quiet style it uses for "reading". When the list arrives the wanted id is selected, or, if the
  hub has no such request, the pane says `no change request <id>` and the list stays with nothing selected. That
  is the wrong-state flash this rule exists for: today `crLoad` would show the first request and then jump.
- An item page waits the same way, on the items list.
- A card attaches through `waitAndAttach`, the restore's own path, with its 90 s wait for a card whose runner is still
  coming up. A card popped out elsewhere is not taken back, as the restore does not.
- `tab=hubrepos` waits for the hub probe, since that tab exists only on a hub. No hub means the board tab and a missing
  mark.

**The readiness mark.** When every part of the route has been applied or given up on, `<html>` gets
`data-route="ready"`, and `data-route-missing="<param> <param>"` names what could not be honoured (an unknown tab, a
request the hub does not have, a card that never came). It is set once per load and again after each `popstate`. A
test waits for that attribute and then asserts, with no click and no guess at a timeout for each step.

A missing selection is replaced out of the URL once the mark is set, so a reload does not try again forever. The
rest of the route stays.

**Precedence on load:** the URL's route, then `restoreWhereYouWere` only for what the URL does not name, then
`landFromURL`. A route with `tab` does not restore the remembered view at all. The first history entry is the result,
written with `replaceState`, as `boot.js` does today.

`landFromURL` stops stripping the whole query: it takes out `land`, `view`, `key` and `why` and keeps the rest, so
a landing on a routed URL does not throw the route away.

### 4. `?crmock=1` with a route

They combine, in either order: `/?crmock=1&tab=hubrepos&repos=requests&cr=cr-0003`. `routeURL` keeps `crmock` on
every push and replace, so the mock stays on through every move of that visit (`mockOn` reads `location.search` on
every call, so a route that dropped it would turn the mock off halfway through a test). The mock's "Turn off" already
removes only `crmock` and reloads, which now lands on the same route, live.

The mock answers through `call` like the hub, so a mocked route reaches the readiness mark the same way and a test
does not branch on it. The hub probe needs a hub in the mock too: the headless harness's existing hub mock is enough,
and a route test that has none expects `data-route-missing="tab"`.

## What does not change

- Pop-out windows: `termOnly` still returns early from everything here. Their address is their card.
- `localStorage`: still written, still the answer to "where was I yesterday" for a bare `/`.
- The phone: `/m/` keeps its own history states (`mcr`, `mcard`, `mdocs`, ...). The redirect already carries the
  query along, and `/m/` ignores parameters it does not read. Phone routes are a follow-up, and `phoneNudge` splits on
  whatever `/m/` offers, or waits for that follow-up.
- Notification landing: same URL, same one-shot behaviour, minus the over-eager strip.

## Build, in phases

**P1. The route for tab, card and item.** `routeURL`, `routeRead`, the push/replace rule in `navPush`, `popstate`,
the load order in `boot.js`, the readiness mark, `landFromURL`'s strip, the `board.js` header. Headless units:
`deepLinks` (load each form, wait for the mark, assert the tab and the selection, an unknown tab is missing),
`backForward` extended to assert the address after each step, and `bootClean`.

**P2. Repos and change requests.** `repos`, `cr`, `crs`, the pending selection in `changereq.js`, the hub probe wait.
Headless: a route to a closed request with `crmock=1`, back and forward across two requests and a sub-view, a request
the mock does not have, and a flash check: a `MutationObserver` on the detail pane records every request id it shows,
and the only one allowed is the linked one.

**P3. The splits** (QA's): `changeReq` becomes units that each load one URL and assert one page. Not this design's to
plan beyond the URL form above.
