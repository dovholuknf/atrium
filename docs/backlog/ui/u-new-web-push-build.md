# u-new-web-push-build. Build web push for the phone

Status: all seven steps built by 2026-10-05, checked against a real hub. Left: the real phone check by clint and the
@review pass. Was HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G12 of docs-deps. Owned by @ui, with a hub
endpoint from @runtime.

## What is missing

A build item. The design is accepted (`docs/rnd/web-push-design.md`) and no queue holds the build.

## Why it is needed

The phone board only alerts while it is open. Web push is the way a closed phone hears that a card needs input.

## Depends on it

Nothing else picks it up. `rd-new-security-review` notes that web push needs HTTPS and a service worker, so the
design's answer on that comes first.

## Done looks like

- A service worker and a subscribe control on `/m`.
- A hub endpoint that stores subscriptions and sends through the existing notify sink (f-017), with the board's
  notification rules and not a second set.
- Subscriptions removed when the browser drops them.
- Tests for the subscribe, the send and the expiry. A real-phone check by clint.

## Built 2026-10-04: the board half

Branch `claude/u-new-web-push-build`. The `push` handler and the tap path are in `internal/api/web/sw.js`, the switch is
`internal/api/web/m/js/push.js` with `m/push.css`, in the notifications sheet under the bell. The check is
`scripts/check-web-push.js` and the shots are in `docs/screens/u-new-web-push-build/`. The switch is built against the
contract below, mocked in the check. Until the hub serves it, the row reads "push is not turned on at this hub".

Not done in the board half: the desktop gear's subscription list with remove, the editable device label, and the
operator's on and off switch. Those wait on the hub routes.

## The contract the board half speaks

All under `/_hub/push/`, JSON, as the design says.

- `GET /_hub/push/key` answers `200 {"key": "<base64url of the 65 byte P-256 public point>"}`, or `404` while push is
  off.
- `POST /_hub/push/subscriptions` takes `{endpoint, keys: {p256dh, auth}, label, origin}`. It answers `201 {id}`, or a
  non-2xx with `{error}` whose sentence the page shows as is. The hub is to send the one test push after a `201`. The
  cap sentence is "8 devices already get alerts. Remove one in the desktop gear first." as a `409`.
- `DELETE /_hub/push/subscriptions` takes `{endpoint}`. A `404` is taken as already gone.
- The push payload is JSON `{title, body, tag, path}`. `title` is the card's name cut to 60, `body` is one of the four
  phrases, `tag` is the card id (`room~id`), and `path` is the card's `/m/alias/<alias>` path, or the form `card.js`
  opens by id with no alias lookup, which is `/m/` followed by `#term=<room~id>`. The worker holds `path` to `/m`
  whatever it is sent.

## Built 2026-10-04: the hub half

Rebased onto claude/main. Steps 1 to 6 of the plan below, standard library only, no new module.

- Step 1: migration `0011_push_subscription` and `internal/hubstore/push.go` (cap 8 with no eviction, repeat endpoint is
  one device, per-row failure count). Key, contact and switch are `hub_setting` rows `push.vapid_private`,
  `push.vapid_sub`, `push.enabled`.
- Step 2: `internal/link/webpush.go`, RFC 8291 and RFC 8292. `TestRFC8291AppendixA` reproduces the RFC's body byte for
  byte. `push_test.go` has a fake push service that decrypts with a browser key pair and verifies the JWT.
- Step 3: the allowlist in `webpush.go`, table test in `webpush_test.go`. Checked at subscribe and on every send.
- Step 4: `Notifier.AddSink` in `notify.go` with per-sink failure counts (`SinkFailures`) and `Arm` for the shared
  seeding. `Push`/`pushSink` in `internal/link/push.go`: Topic (card id hashed to 32 chars), TTL 3600, Urgency, padding
  to 256, burst cap summary, 10 s per send, no redirects, bounded read, endpoint kept out of errors.
- Step 5: 404 or 410 deletes the row. Three failures in a row switch that subscription off with its reason.
- Step 6: `internal/link/pushapi.go`, wired in `atrium_run.go` with `SetPush`. Operator only (`edge.LocalOperator`): GET
  and PUT `/_hub/push`, `POST /_hub/push/test`, `DELETE /_hub/push/subscriptions/<id>`. Open: `GET /key`, and the
  share's subscribe and endpoint-bodied unsubscribe. The `X-Forwarded-For` 403 test is
  `TestPushOperatorRoutesAreLocalOnly`. Writes also pass the cross-origin check. A new device is put in the audit feed
  and raised as a question growler on the first attached room, and ended when removed.

Choices to know. Key rotation removes every subscription, since a push service refuses them under a new key. The
share's DELETE by endpoint is allowed whether push is on or off. The path in a payload is `/m/#term=<room~id>`. The
test push after a 201 is sent in the background. A switched-off subscription stays listed until removed or until it
subscribes again.

Tests: `go test ./internal/link/... ./internal/hubstore/...` pass for everything push. Two failures in `internal/link`
are not from this work: `TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess` fails the same on claude/main (no `gh`
login here), and one full run hung 5 minutes in `TestAPrEventArrivesUntouchedFromASingleRoom`, which passed when run
alone. `scripts/check-web-push.js` was not re-run against the real routes.

## Built 2026-10-05: step 7, the phone name, the real-route check

- The gear row is `js/hubpush.js` and `css/hubpush.css`, markup in `index.html` in the notifications pane under the
  notify row, loaded from `settings.js` on open. It reads `GET /_hub/push` and is hidden on any non-2xx or for a guest.
  Switch, contact, list with remove by id, test to all, and a new key behind a confirm.
- The phone name is an input on `/m` shown while subscribed, kept in localStorage `atrium.push.label`, and sent as a
  second `POST` of the same endpoint, which the store already treats as one device. The hub now sends the test push only
  when the subscribe added a device, so a rename does not buzz (`pushapi.go`).
- `scripts/check-web-push.js` with `HUB=http://127.0.0.1:<port>` also runs against a real hub, started as
  `HTTPS_PROXY=http://127.0.0.1:9 atrium run --no-room --addr .. --link .. --atrium-dir <scratch> --board
  internal/api/web`. The dead proxy matters. Without it the hub's test push reaches Google with the made-up endpoint, the
  answer is 404 or 410 and the row is dropped, as designed. It passes: phone on, name, off, the gear list, contact,
  remove, switch, key rotation, the 403 with `X-Forwarded-For`. The fake keys are now the RFC 8291 test pair, since the
  real hub rejects a key that is not a 65 byte point. Nothing here found a hub bug.
- Shots in `docs/screens/u-new-web-push-build/`: `before-real-gear.png` and `after-real-gear.png` (also
  `-gear-empty`), `before-off.png`, `after-on.png`, `after-named.png` and `after-real-named.png` for the phone.
- Push tests by name pass (`go test ./internal/link/ ./internal/hubstore/ -run 'Push|RFC8291|Webpush|Allowlist'`).
  The full package was not run.
- Seen and not mine: the notify status in the gear prints "true held back because a board was open", a
  `hnPaint` bug in `hubnotify.js` (`suppressed` is a bool).

Left:

1. The real phone check by clint (Brave on Android, an iOS home screen app).
2. The @review pass for both halves.

## The hub plan (as accepted, steps 1 to 6 now built)

It stays inside `internal/link` and `internal/hubstore` and needs no new Go module. It was not built tonight because it
includes VAPID key handling, which the brief named as a reason to stop and plan. In order:

1. **Store.** A `push_subscription` table in a new hub migration at the end of the slice: `id`, `endpoint` (unique),
   `p256dh`, `auth`, `label`, `origin`, `created_at`, `failures`, `disabled_reason`. The VAPID private key and the
   `sub` contact go in `hub_setting` under `push.vapid_private`, `push.vapid_sub` and `push.enabled`. The private key
   never leaves the hub and is never logged or returned.
2. **Keys and crypto, standard library only.** `crypto/ecdsa` P-256 for the VAPID key, made on first enable. RFC 8291
   `aes128gcm` with `crypto/ecdh`, `crypto/hkdf` and `crypto/aes`, padded to 256 bytes. RFC 8292 JWT (`ES256`) in
   `Authorization: vapid t=..., k=...`, with the `sub` contact (default the project URL, never an email). Tests: the
   RFC 8291 appendix A vector decrypts, and a fake push service given a browser key pair decrypts what the hub sends
   and checks the JWT.
3. **Allowlist.** Parsed with `net/url` and never the raw string: https, host exact or a suffix with its leading dot
   (`fcm.googleapis.com`, `.push.apple.com`, `updates.push.services.mozilla.com`, `.notify.windows.com`), port absent
   or 443, no userinfo, no IP literal. Checked at subscribe and again on every send. No redirects, bounded response
   read. Table tests for `evilpush.apple.com.example`, `xpush.apple.com`, `user@`, `:8443` and IP literals.
4. **Sink.** A `PushSink` behind the existing `Sink` interface in `notify.go`, so the trigger, the dedupe, the seeding,
   the desktop-tab quiet and the drop-oldest queue are the board's one set of rules. `Notifier` takes a list of sinks
   and `record` counts per sink, because the command sink's three failures must not switch push off and the other way
   round. `Notice` stays four fields. Headers: `Topic` the card id, `TTL` 3600, `Urgency` high for a permission and
   normal otherwise. The burst cap is more than 5 in 2 minutes to one subscription becomes one "N cards want you"
   summary tagged `atrium-summary` until 2 quiet minutes pass. Ten seconds per send.
5. **Expiry and failure.** A `404` or `410` deletes the row. Three failures in a row switch that subscription off with
   its reason, shown in the gear.
6. **Routes** in a new `internal/link/pushapi.go`, wired like `serveNotify`. `PUT /_hub/push` (on and off, the `sub`
   contact, key rotation) and the test push to every subscription are `edge.LocalOperator` only. The share may subscribe
   and unsubscribe its own browser while push is on, and `DELETE` needs the endpoint in the body. Listing and removal by
   id are operator only. At most 8 subscriptions, never evicting the oldest. Every new subscriber puts "a new device
   subscribed to alerts: <label>, from <origin>" in the desktop growler. Test: with `X-Forwarded-For` set,
   `PUT /_hub/push` is 403.
7. **Desktop gear.** The subscription list with remove and the on and off switch, which is a second board change after
   the routes.

Both halves go through @review, since the hub adds an outbound call and the worker adds a handler a push service can
wake.
