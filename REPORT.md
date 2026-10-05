# u-new-keepalive-pie report

## What changed
- `internal/api/web/js/keepalive.js`: `kaModel` computes `pie` (degrees, 15 degree steps) and `due`. The chip sets
  `--pie`. `kaArm` also wakes once a minute while a pie fills.
- `internal/api/web/css/cards.css`: conic-gradient pie, amber ring when due, struck empty circle when stopped.
- `scripts/test-board-headless.js`: cacheChip section 5 tests the fraction, each state's look and tooltip, and that
  a timer is armed. The dot size bounds went from 12 to 16px.
- Changelog, and the design note in the item file.

## Design notes
- No room change: `last_refresh_at`, `next_refresh_at` and `warm_until` are already on the view (r-032). Nothing is
  needed from @runtime.
- Place: unchanged, the chip row of the card face and the phone rows. 10px became 14px.
- A card with no `next_refresh_at` keeps the old dot. Cold and stopped have their own looks.

## Tests
`NODE_PATH=<main checkout>/node_modules HEADLESS_ONLY=cacheChip,cacheLine node scripts/test-board-headless.js`
passes. No Go code changed.

## PNGs
`docs/screens/u-new-keepalive-pie/{before,after}-stack-1400x900.png`: cards for start, half, about to fire, stopped,
cold and first-cycle. The before shot is the old code under the new fixtures. Only the stack view was shot.
