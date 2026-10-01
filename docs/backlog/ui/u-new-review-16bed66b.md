# Review of 16bed66b (@ui u-m-redirect: a phone opening the board lands on /m)

Reviewed by @review, 2026-09-30, from `git diff 0ccc5093 16bed66b`, read only. Hub side. Board and headless checks
are @ui's, and I ran none.

## What holds

- **No open redirect.** Every target is a fixed `/m` path: `/m/`, `/m` plus a path that matched
  `^/alias/[^/]+/?$` or `^/room/[^/]+/[^/]+/?$`, or `/m/` plus a hash that starts `#term=`. The hash stays a
  fragment whatever follows it, so nothing in the address can point the redirect off the origin. `replace` keeps
  Back from looping.
- **Pop-outs are left alone.** A window with an opener, or whose name starts `atrium-term-` (which solo.js now sets,
  e7555101), is never moved. Neither is anything already under `/m`.
- **The opt-out.** `atrium.m.desktop` is set only by the "desktop board" link on /m and cleared only by "phone view"
  in the drawer. That button shows only when the opt-out is set. Every test context presets the opt-out, so the
  suite still sees the desktop board.
- **`/m/#term=<id>`** is decoded, replaced with `/m/` in history, and opened through the same `whenHeld` path as a
  readable card address.
- The head script is wrapped in `try`, so a browser without `matchMedia` or storage just stays on the board.

## Findings

### Low

1. **A notification tap on a phone lands on /m home, not on the card.** A notification click and the toasts' own
   links arrive as `/?land=<id>&view=…&key=…` (toasts.js:99). The head script sends `/` to `/m/` and drops
   `location.search`, so the card or permission the notification was about is lost. Desktop notifications from the
   phone board can do this today, and the Web Push design (5e03257c) would make it the main path. Mapping `land` to
   `/m/#term=<land>` keeps the tap on its card. The permission `key` has no /m target yet.

### Nit

2. `/room/<name>` writes `atrium.room` into this phone's localStorage from the address, so any link can change which
   room the phone shows. It is the same room picker a tap would set, and nothing more.

HUB DEPLOY OK 16bed66b

## Re-read of 45dba246 (@ui, low 1)

`git diff 16bed66b 45dba246 -- internal/api/web`, read only. On `/` or `/index.html`, `land` becomes
`#term=<encodeURIComponent(land)>`, `land`, `view` and `key` are dropped, and the rest of the query is re-encoded
by `URLSearchParams` after `/m/`. The target still starts with `/m/` on the same origin. `/?view=perms` with no card
goes to `/m/`. Low 1 is closed. Nit 2 stays, as @ui chose.

HUB DEPLOY OK 45dba246

## Review of 0b91385f (@ui u-m-redirect2: the redirect that did not fire on clint's phone)

`git diff 98e5ae9f 0b91385f -- internal/api/web`, read only.

- The opt-out moves to `sessionStorage`, so it belongs to one tab, and the old `localStorage` value is removed on
  every board load. The "desktop board" link and "phone view" both use the new store.
- `?why=1` writes its line through `textContent`, so a hostile `window.name` stays text. `why` is removed from the
  query that is carried to /m, and the 4-second delay applies only with `?why=1`.
- The targets are unchanged: still fixed `/m` paths on the same origin.

### Low

3. **A touchscreen laptop with a small window now counts as a phone.** The test is now `(coarse || any-coarse ||
   maxTouchPoints > 0)` and `min(innerWidth, innerHeight, screen.width) < 600`. A Windows touch laptop or a Surface
   passes the first half, and any board window under 600 px on either side passes the second. That covers a window
   snapped to a corner, a short window with devtools docked, or a narrow side-by-side. The desktop board then goes to
   /m, and because the opt-out is now per tab, it happens again in every new tab. Checking the screen rather than the
   window, `min(screen.width, screen.height) < 600`, would still catch every phone and leave a small window on a big
   screen alone.

Quality: after the Sonnet switch. The diagnosis tooling (`?why=1`, a reason for every outcome, a test case for each)
is careful. The widening was not weighed against touch-capable desktops, which is the kind of second-order check
I would have expected.

HUB DEPLOY OK 0b91385f

## Re-read of 06c7c758 (@ui, low 3)

`git diff 0b91385f 06c7c758 -- internal/api/web`, read only. The size test is now `min(screen.width, screen.height) <
600`, so a small window on a big touch screen stays on the board, and a phone (412x915 CSS px) still goes to /m. Both
are CSS pixels, so a high-density phone reads small, as it should. Low 3 is closed. No findings.

Quality: after the Sonnet switch. The one-line fix the review asked for, and the comment says why. No drop seen.

HUB DEPLOY OK 06c7c758
