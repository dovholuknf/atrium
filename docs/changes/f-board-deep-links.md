# Board deep links

The view, the repos sub-view, the change request and its open or closed tab are carried by the address, so a link lands
there with no click chain, back and forward step through it, and a headless test can be "load this URL, assert".

## Design

**The hash, as a route.** `#/<view>[/<sub>[/...]]`. The existing scheme is kept, not replaced:

- `#term=<id>` stays the pop-out's form. A route always starts with `#/`, so the two never share a string, and a solo
  window ignores routes as it ignores `popstate` today.
- The query keeps its own jobs: `?land=` and `?view=` (a notification click) and `?crmock=1` (the mock). A route lives
  in the hash, so `?crmock=1#/hubrepos/requests/closed/cr-1` just works and neither parser reads the other's half. The
  hash is also never sent to the hub.

| Address | Lands on |
| --- | --- |
| `#/pulls`, `#/history` | that view (any name in `VIEWS`) |
| `#/hubrepos/<sub>` | repos, with `shelf`, `ledger`, `feed`, `requests` or `branches` (`#/hubrepos` is `shelf`) |
| `#/hubrepos/requests/<tab>` | requests, `open` (the default) or `closed` |
| `#/hubrepos/requests/<tab>/<id>` | requests, that tab, that change request selected |

**Unknown parts.** An unknown view falls back to the board, an unknown sub-view to `shelf`, an unknown tab to `open`.
The dropped part is named in `data-route-missing` (below) and the address is replaced with what was honoured.

**Push or replace.** Same rule `navPush` already has: a move somebody makes pushes one entry, and the first entry
replaces. Switching view, switching sub-view, flipping the tab and choosing a request each push. Anything the board does
on its own replaces: the default selection of the first request after the list arrives, and correcting an address.
`navState()` gains `sub`, `reqTab` and `req`, and the hash is written from it by the same
`history.pushState(state, "", url)` call, so the "unchanged state pushes nothing" check still stops repaints stacking
entries. `switcher.js`'s `replaceState` for `#term=` is untouched.

**The readiness mark.** `<html>` carries `data-route="pending"` while a route is being applied and `data-route="ready"`
when it was applied or given up on, set once per load and again after each `popstate` or `hashchange`.
`data-route-missing="<part> ..."` names what could not be honoured (`view=nope`, `sub=nope`, `tab=nope`, `id=cr_1`) and is
absent when everything was. A test waits on that attribute and nothing else, never on a timed wait per step. For a
request id the mark waits for the list and the request's page to be read.

**Which restore wins.** `#term=` and `?land=` first, then a route, then `localStorage` (`atrium.view`,
`atrium.reposView`). A route that names a view skips the stored view and the stored sub-view entirely. A bare address
still answers "where was I yesterday" from `localStorage`. The route is applied at the top of `bootBoard`, before the
roll call and so before the boot cover comes off (`doneSwitching`): the first frame is the linked view, never the
remembered one and then the linked one. `restoreWhereYouWere` does nothing to the view when a route landed (it still
re-attaches the remembered terminal when the route is `#/terms`).

**Hash against popstate.** Going back between entries that differ only in their hash fires `popstate` and
`hashchange`. The board records the hash it last applied or wrote, and `hashchange` does nothing when the hash equals
it, so one press of back applies once. A hash typed into the bar in this tab arrives as `hashchange` alone, and is
applied without pushing.

**A deep link to data that has not arrived.** The view and the sub-view are markup, so they switch at once. The request
id is held as pending, nothing is selected and `crLoad`'s auto-pick of the first request is off while it is pending, and
the pane says "reading" until the list lands, so there is no frame of the wrong request. When the list lands an id that
exists wins over the tab (a `closed` id under `open` flips the tab, as a click already does).

**An id that does not exist** does not fall back to the first request, because a silent fallback hides a broken link.
The pane says "No change request <id>.", nothing is selected, the id goes in `data-route-missing`, and the address is
replaced with the tab-only route.

**`?crmock=1`.** Orthogonal. The mock answers the same reads, so the route resolves against the mock's ids. Every
pushed URL is built from `location.pathname + location.search + hash`, so the query survives.

## Out of scope

- The items page (`itemsOpenId`) and the terminals view's card keep living in the state object only.
- The phone board (`m/`) has its own scheme. The redirect in `index.html` keeps only `#term=` and drops any other hash,
  so a route opened on a phone lands on `/m/`. Nothing there changes.

## Pictures

`img/f-board-deep-links/before.png` is `#/hubrepos/requests/closed/cr_6` opened without route support, which lands on the
default view. `after.png` is the same address now.

## Test plan

Headless units in the `route` bucket load a URL and wait on `data-route="ready"`: `deepLinkView` (every view, unknown
view, a bare address), `deepLinkRequests` (sub-view, tab, id, an id that does not exist, an id before the list arrives,
`?crmock=1`) and `deepLinkBack` (push and replace, back and forward, one apply per step, a hash typed in the bar).
`changeReq` and `phoneNudge` can then be split onto it. `backForward` keeps passing unchanged.
