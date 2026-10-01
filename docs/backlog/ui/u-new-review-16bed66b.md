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
