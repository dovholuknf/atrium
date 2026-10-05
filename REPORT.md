# u-new-web-push-build: report

Incomplete. The board half is built and checked. The hub half is planned, not built.

## What changed

- `internal/api/web/sw.js`: a `push` handler that always shows a notification (title cut to 60, tag is the card id, no
  action buttons, never silent), a `pushsubscriptionchange` handler, and a tap on a push that opens the card's `/m` path
  (the open `/m` window is navigated, else a new one opens). A path off `/m` is turned into `/m/`.
- `internal/api/web/m/js/push.js`, `m/push.css`, `m/index.html`: the "phone alerts" row in the notifications sheet. It
  asks permission from the tap, reads `GET /_hub/push/key`, subscribes, `POST`s the subscription, and `DELETE`s it with
  the endpoint on the way off. The row says why when it cannot work: insecure page, iPhone tab not on the home screen,
  hub with push off, permission denied, browser refusal (Brave's setting is named), hub refusal (its sentence verbatim).
  A test push that does not arrive in 30 seconds names Brave's setting too.
- `scripts/check-web-push.js`: the headless check.
- `docs/backlog/ui/u-new-web-push-build.md`: the contract the board speaks and the hub plan.
- `changelog/ui/2026-10-04-u-new-web-push-build.md`.

## Why the hub half stopped

The brief names VAPID key handling as the line. The hub half is also the first outbound call from the hub to a third
party, RFC 8291 encryption, a migration, a second sink with its own failure count and the burst cap. It needs no new Go
dependency, so nothing is waiting on clint's yes. The plan is in the item, in seven steps.

## Tests

- `NODE_PATH=/d/worktrees/claude/atrium/u-new-context-bar-on-rows/node_modules node scripts/check-web-push.js docs/screens/u-new-web-push-build`
  passes. It covers: off, subscribe (permission asked once from the tap, `userVisibleOnly`, the 65 byte key, one POST
  with endpoint, both keys and a label), unsubscribe (DELETE carries the endpoint, the browser unsubscribes), an
  already-subscribed browser reading as on, permission denied (nothing posted), browser refusal, hub refusal (sentence
  verbatim, browser half undone), hub with push off, an iPhone tab, and the service worker's push and click code.
- The service worker is run in a stand-in scope (`vm`) and not a real worker, because headless Chromium denies
  notification permission to a worker whatever the context grants. The notification shown, replaced by tag, the path
  rules and the tap are checked there. The real-phone check is clint's.
- `go test ./internal/api/` passes (no Go changed, the web tree is embedded). No Go tests for the store, the send or the
  expiry exist because the hub half is not built.

## PNGs (`docs/screens/u-new-web-push-build/`, real /m at 390 by 844, headless)

- `before-off.png`: claude/main, the sheet with no control. This is the only before there is.
- `after-off.png`, `after-on.png`, `after-refused.png`. The "on" shot is the fake `PushManager` and a mocked hub, since
  neither exists here.

## Left

- @runtime: the hub half, per the item. Then the desktop gear list and on/off switch (@ui).
- Editable device label, which the design asks for. The page sends a guessed one ("Android phone (Brave)").
- clint: the real-phone check, Brave on Android first (its "Use Google services for push messaging" setting), then an
  iOS home-screen web app.
- @review on both halves.
