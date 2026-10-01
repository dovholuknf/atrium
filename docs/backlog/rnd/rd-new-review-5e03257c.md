# Review of 5e03257c (@rnd: docs/rnd/web-push-design.md)

Reviewed by @review, 2026-10-01. Design only, read for what stage 1 will need to get right.

## What holds

- **The LP1 split is the right one.** Enabling push, the VAPID keys, the `sub` contact and the test-to-all are
  operator-only. A share user may only subscribe and unsubscribe. Anyone past the share's password can already
  approve permissions, so a subscription adds no reach.
- **The payload is bounded on purpose.** It carries four plain fields, padded to 256 bytes, and never the command,
  the question, reply text or the recap. The overlay rule holds: the VAPID key is atrium's own, and the subscription
  secret is the browser's, handed over for this use.
- **The allowlist is the outbound bound, and its stated rules are right:** HTTPS only, redirects not followed, the
  response read bounded.

## What stage 1 should spell out

1. **"Its own browser" needs proof on unsubscribe.** `DELETE /_hub/push/subscriptions/<id>` from the share must not
   take an id alone. Ids are small or guessable, and a list readable over the share would hand them out, so anyone
   past the share's password could drop clint's phone. Require the request to carry the subscription's `endpoint`,
   and delete only on an exact match. Only the browser holding the subscription knows it. Keep any listing of
   subscriptions operator-only. The gear shows a phone its own subscription from `pushManager.getSubscription()`,
   not from the hub.
2. **The allowlist match must be exact.** Check the parsed URL's host, not the string: `fcm.googleapis.com` equal,
   and `*.push.apple.com` as a suffix with the dot (`.push.apple.com`), so `evilpush.apple.com.example` and
   `https://fcm.googleapis.com@evil.example/` both fail. Also require port 443 or none, no userinfo, and no IP
   literal. Re-check on every send, not only at subscribe, in case the list changes.
3. **The 8-slot cap is also a way to lock clint out.** A stranger past the share's password can fill all 8 slots, and
   clint's phone then cannot subscribe. The growler line makes it visible. The gear should let the operator remove
   any subscription, and a refused ninth subscribe should name the cap. Nothing more is needed, since the share's
   password is the real boundary.

None of these change the shape. They are the precise rules stage 1 is reviewed against.
