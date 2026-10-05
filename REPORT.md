# u-new-context-bar-on-rows: report

## State: incomplete

The row bar and its colour ramp are done. The per-runner limit layer is not built, its plan is in the item.

## What changed

Most of the item had already landed (thin bar on rows and cards, shared `ctxMeter`, tick, single pulse, tooltip).
This pass:

- `internal/api/web/js/board.js` `ctxLine`: colour by fraction of the limit. Green (theme teal) under 60%, yellow from
  60%, red from 85%, full red with one pulse at or past the limit. Before, yellow followed the daemon's 150k warn flag.
- `scripts/test-board-headless.js`: ctxLine and landThePlane sections now assert the 60%/85% steps, three distinct
  fills, 85% equals the danger fill, and the tooltip text on the 52%, 67% and 85% rows. The screenshot hook now draws
  green, yellow, red and at-limit rows.
- Item `docs/backlog/ui/u-new-context-bar-on-rows.md`: Design note and the per-runner plan.
- `changelog/ui/2026-10-04-u-new-context-bar-on-rows.md`.

No Go change: the row already carries `context_size.tokens`. No new polling, no looping animation.

## Design notes

- No extra "limit" chip. The LAND badge was removed on purpose after the ctx-badge review (contrast), and the test
  asserts there is none. The full red pulsing line and the amber mark mark the limit. Easy to add back if wanted.
- Board cards share `ctxLine`. The stack list has only the amber mark. /m rows not touched.
- The limit is the land-the-plane line (browser setting, never below the card's threshold).
- "Green" is the theme's teal, the same as the popover meter.

## Not built

Per-runner limit layer (store, API, three editors). Plan in the item: `context_limit_k` on the harness row, resolved
by the daemon into `context_size.threshold_k` plus a `source`, then the editors in the UI.

## Tests

`HEADLESS_ONLY=ctxLine,landThePlane,contextSize,bootClean node scripts/test-board-headless.js` passes
(after `npm install` and `npx playwright install chromium`). No Go tests, no Go change.

## PNGs (`docs/screens/u-new-context-bar-on-rows/`)

`before-2000-{paper,graphite}.png`, `before-390-{paper,graphite}.png`, `after-...` the same. Rows: plain, green (20%),
yellow (70%), red (90%), at limit (130%). Before, the 70% and 90% rows were not yellow and red as the item asks.
Made with `ROWFLOOD_SHOTS=<dir> ROWFLOOD_TAG=before|after HEADLESS_ONLY=ctxLine`.
