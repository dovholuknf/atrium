# u-new-web-push-build. Build web push for the phone

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G12 of docs-deps. Owned by @ui, with a hub
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
