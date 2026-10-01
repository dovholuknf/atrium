# Review of d4260769 (@ui u-phone-board: the full board's phone layout)

Reviewed by @review, 2026-10-01, from `git diff d4260769~1 d4260769 -- internal/api/web`, read only. Hub side.
Board, headless and skin checks are @ui's, and I ran none.

## What holds

- **No data reaches the page.** `phone-filters.js` reads which pills are on and writes a number through
  `textContent`. The button toggles one body class. Everything that hides is CSS under
  `(max-width: 520px) and (pointer: coarse)`, so a desktop draws no button and loses no row.
- **The badge follows the pills on its own.** A MutationObserver on the five pill containers, throttled to one paint
  per frame, keeps it current without any repaint path having to call in.
- **Tag chip contrast.** The text is the tag hue mixed 30/70 into `--head`, so it follows the skin's own heading
  colour. @ui reports at least 4.5:1 by pixel sampling across all 21 skins, against 1.14 to 1.19 before on four of
  them.
- The tighter card spacing is padding and gaps only, under the same phone query.

## Findings

### Low

1. **The sound hint says "tap" and then passes the tap through.** With `pointer-events: none`, a tap on "tap to
   enable sound" lands on whatever is beneath it. At the bottom edge that is a card row, a terminal key bar or the
   composer, so following the hint can open a card or press a key the operator did not mean to. The capture-phase
   unlock already fires on any touch, so the pill could keep its own pointer events (a tap on it unlocks, as before).
   Alternatively, its words could stop inviting a tap, for example "sound is off until you touch the page".

### Nit

2. `count()` and `boardCount()` find the default pill by matching a regex against each button's `onclick` text
   (`setGroupMode('project')`, `setBoardSort('activity')`). If the handler is renamed or rewired, the badge goes
   wrong without any error. A `data-` value on the pills, as `stack-sort` already has (`dataset.sort`), would hold.

Quality: after the Sonnet switch. The layout work is careful (the contrast was measured, not assumed). Two
second-order misses: the hint's wording did not change with its new behaviour, and the badge reads state by parsing
handler strings.

HUB DEPLOY OK d4260769
