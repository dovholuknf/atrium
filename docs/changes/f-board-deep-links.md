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
| `#/hubrepos/<sub>` | repos, with `shelf`, `ledger`, `feed`, `requests` or `branches` |
| `#/hubrepos/requests/<tab>` | requests, `open` (the default) or `closed` |
| `#/hubrepos/requests/<tab>/<id>` | requests, that tab, that change request selected |

An unknown view or sub-view is dropped to the nearest known prefix (`#/nope` is the board, `#/hubrepos/nope` is the
remembered sub-view).

**Push or replace.** Same rule `navPush` already has: a move somebody makes pushes one entry, and the first entry
replaces. Switching view, switching sub-view, flipping the tab and choosing a request each push. Anything the board does
on its own replaces: the default selection of the first request after the list arrives, and correcting an id that
turned out not to exist. `navState()` gains `sub`, `reqTab` and `req`, and the hash is written from it by the same
`history.pushState(state, "", url)` call, so the "unchanged state pushes nothing" check still stops repaints stacking
entries. `switcher.js`'s `replaceState` for `#term=` is untouched.

**Which restore wins.** `#term=` and `?land=` first, then a route, then `localStorage` (`atrium.view`,
`atrium.reposView`). `localStorage` still answers "where was I yesterday" for a bare address. A route is applied inside
`restoreWhereYouWere`'s slot, so there is one landing and not two restores fighting. A manual edit of the hash in the
same tab arrives as `hashchange` and is applied the same way, without pushing.

**A deep link to data that has not arrived.** The view and the sub-view are markup, so they switch at once. The request
id and tab are written into `cr.sel` and `cr.tab` before `crLoad` runs, and the pane already says "reading" until
`cr.loaded`, so there is no frame of the wrong request. When the list lands, an id that exists wins over the tab (a
`closed` id under `open` flips the tab, as a click already does). An id that does not exist falls back to the tab's
first request and the address is replaced with the corrected one, so a stale link does not stay stale in the bar.

**`?crmock=1`.** Orthogonal. The mock answers the same `/v1` reads, so the route resolves against the mock's ids. The
`crmock` query is kept on every push (the URL is built from `location.pathname + location.search + hash`), and the
existing code that strips it for non-mock loads is unchanged.

## Out of scope

The items page (`itemsOpenId`) keeps living in the state object only. The phone board (`m/`) has its own scheme.

## Test plan

Headless units load a URL and assert, one small page each: `deepLinkView`, `deepLinkRequests` (sub-view, tab, id,
unknown id, id before the list arrives), `deepLinkBack` (push, replace and back through them). `changeReq` and
`phoneNudge` can then be split onto it. `backForward` keeps passing unchanged.
