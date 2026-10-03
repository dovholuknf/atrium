# u-new-row-flood: a context figure no longer floods or badges a terminals row

Filed from the desktop board's terminals list, 2026-10-02.

## What and why

Two things on a terminals row (and the stack row, which shares the mark) looked wrong:

1. The red `LAND` badge past the land-the-plane line. Atrium now clears an agent card on its own at that line on
   every room, so the badge told the operator nothing they could still act on.
2. The context bar was drawn as a flood behind the whole row (a layer at `z-index: -1` tinted with the heat colour,
   plus a red border past the land line). On a row with a context figure that painted a lighter band over the title
   and path, hid the left accent bar's edge, and made the row look washed out next to a plain one.

## What changed

- `ctxWarnMark` (`js/board.js`) no longer draws `LAND`. It draws the amber mark at the warn line, as before. Past the
  land line a row therefore has the amber mark and the danger-coloured context line, nothing else. The amber mark is
  a separate element at the warn line, so it stays. The phone (`/m`) has no row of its own for this: it uses the same
  `ctxWarnMark`, so it loses the badge too. `.chip.ctxland` css is gone.
- `.peek-bar.ctxline` (`css/cards.css`) is now a 3px strip along the bottom of the row, inset 10px from the corners
  so it never touches the left accent bar or the border. No fill behind the title and path, no stacking context, no
  danger border. A transparent hit area above the strip carries the tooltip. Heat steps kept: neutral, amber at
  warn, danger at land. The one-off pulse became a one-off glow, which still reads on a thin bar.
- Unchanged: `landOver` and `landTip` (auto-clear, the details), the details' meter in `js/peek.js`, `ctxMeter`'s
  markup.

## Tests

`HEADLESS_ONLY=ctxLine`, `landThePlane`, `contextSize` (`scripts/test-board-headless.js`): no LAND chip at 130% of
the line (desktop, stack row, 340px); the context line is a 2-4px strip at the row bottom, inset, with no z-index
layer; three different fills for neutral, amber and danger; a row with context has the same background and border as
a plain one. Mutant-checked: LAND restored, the flood css restored and the red border restored each fail.
`ROWFLOOD_SHOTS=<dir> ROWFLOOD_TAG=before|after HEADLESS_ONLY=ctxLine` writes the four-row comparison at 2000px and
390px on paper and graphite.

## What is left

Nothing known. The pulse is the only part of the old design that changed shape rather than going away.
