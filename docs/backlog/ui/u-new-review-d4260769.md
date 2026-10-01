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

## Re-read of 2d56c866 (@ui, low 1 and nit 2)

`git diff d4260769 2d56c866 -- internal/api/web`, read only. The pill is a target again, 40 px tall. Its click calls
`preventDefault` and `stopPropagation`, then unlocks and hides it. A press on the pill is marked from the window's
capture phase before the unlock listeners run, and `paintSoundHint` will not hide a pill mid-press, so the click
cannot land on the row that was under it. The badge reads `data-group` and `data-sort`, which `paintGroupSegs` and
`paintBoardSort` now write from their own static keys. Both closed.

Nit: a press that starts on the pill and slides off before release sends `pointerup` and no click or cancel, so
`data-press` stays set. The pill then stays up after sound has unlocked, until it is tapped. Releasing it on a
`pointerup` whose target is not the pill (a slide-off), or on the next press anywhere else, would cover that.
Releasing on every `pointerup` would bring back the fall-through this commit fixed.

Quality: after the Sonnet switch. @ui found the second-order case itself (the hide on pointerup that let the click
fall through) and wrote a test that puts the pill over a row. No drop seen.

HUB DEPLOY OK 2d56c866

## Re-read of 433fa896 (@ui, the slide-off nit)

`git diff 2d56c866 433fa896 -- internal/api/web`, read only. A `pointerup`, `touchend` or `mouseup` whose point lies
outside the pill's rect clears `data-press` and repaints, so the pill goes once sound is unlocked. A release on the
pill is left for its click, so the fall-through fix holds. The next press anywhere else also clears the mark. A drag
off the pill does not fire a click on the row: a moved touch has no click, and a mouse click goes to the common
ancestor. Closed. No findings.

Quality: after the Sonnet switch. It follows the review's suggestion exactly, with a drag test. No drop seen.

HUB DEPLOY OK 433fa896
