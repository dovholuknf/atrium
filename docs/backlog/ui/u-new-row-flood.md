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
  danger border. A transparent 6px hit area (2px above, 1px below the strip) carries the tooltip. Review fixes: it was 11px and
  covered the row's bottom band. A live hub showed the strip through the path line, so every terminals row now
  reserves a 6px band under its content (`.term-list .card` in terminal.css, phone and mini variants too) and the
  strip sits in the bottom 5px of it. Row height no longer depends on having a context figure; the board's cards
  add the band only when they carry a strip. Heat steps kept: neutral, amber at
  warn, danger at land. The one-off pulse became a one-off glow, which still reads on a thin bar.
- Unchanged: `landOver` and `landTip` (auto-clear, the details), the details' meter in `js/peek.js`, `ctxMeter`'s
  markup.

## Tests

`HEADLESS_ONLY=ctxLine`, `landThePlane`, `contextSize` (`scripts/test-board-headless.js`): no LAND chip at 130% of
the line (desktop, stack row, 340px); the context line is a 2-4px strip at the row bottom, inset, with no z-index
layer; three different fills for neutral, amber and danger; a row with context has the same background and border as
a plain one. Also the hit area is 5-7px and a strip row has at least 2px more bottom padding than a plain one. Mutant-checked: LAND restored, the flood css restored the red border, the old hit area and the removed padding restored each fail.
`ROWFLOOD_SHOTS=<dir> ROWFLOOD_TAG=before|after HEADLESS_ONLY=ctxLine` writes the four-row comparison at 2000px and
390px on paper and graphite.

## What is left

Nothing known. The pulse is the only part of the old design that changed shape rather than going away.

## Second pass: back to the 3e9ca7a1 look

The inset 3px strip and the reserved band under every row looked detached and cost row height. The context line is
back to how it was at 3e9ca7a1: a 4px line flush with the bottom edge inside the card (the row clips it to its
corners), gradient fills (teal, amber from the warn line, danger from the land line), the one-off pulse, and the
bar's own tooltip with tokens, limit and window. The band, the `::after` hit area and the padding rules are gone.
The line sits inside the row's existing bottom padding (5-6px), so it never meets the path line or a chip and a row
is exactly as tall with or without it (both asserted in `ctxLine`). Still no LAND badge and no flood.

Real-row check: `ROWFLOOD_REAL=<tasks.json> ROWFLOOD_REAL_SHOTS=<dir> ROWFLOOD_TAG=before|after
HEADLESS_ONLY=ctxLine node scripts/test-board-headless.js` draws the board from a copy of a live `/v1/tasks` answer
(`/tmp/u-row-flood/real/tasks-live-copy.json`, five rows pinned) at 2000px and 390px on paper and graphite.

Review M1: the 4px line sits in the row's bottom padding, `--row-y-xs` = 5px x uiscale x density, which a tight
density (0.72, 0.5, 0.3) or a small text size takes under 4px, so the line ran into the path. The terminals row now
has `padding-bottom: max(var(--row-y-xs), 5px)`. `ctxLine` redraws the rows at density 0.3 and 0.5, uiscale 0.6, and
both together, and runs the same clash walk; removing the `max` fails it. Shots: `/tmp/u-row-flood/real2/`
(before = 6dcb0723, after = this fix, density 1 and 0.3, 2000px, paper and graphite).

Real-row shots, round two (`/tmp/u-row-flood/real2/`): the harness now sets the rows up like an operator's board: themed cards,
room chips (hub mode, `room~id` ids), a parent with nested children, five pinned, a 520px list so titles show, density
1, 0.72 and 0.3, paper and graphite, at 2000px on the desktop board and 390px in a touch, mobile-viewport context
(phone.css applies). The phone's pinned rows are NOT clipped in that layout; the clipped left edge seen when the desktop
board was squeezed to 390px is an artefact of that squeeze, so no `u-new-phone-pinned-clipped` item is needed.

## Third pass: the fold button on the phone, and the pinned heading in the shots

- On the phone grid the fold button (`.tkidfold`) of a parent row was a fifth grid item with no placement, so it
  fell to a third grid row under the path and made that row about 20px taller than a childless one at every
  density. It now has a fifth column at the end of the first line and the name stops short of it (phone.css).
  The real-row harness asserts, per density and skin, that a parent row is as tall as the same row without its
  button; with the old CSS that fails on the phone, with the fix it passes at 1, 0.72 and 0.3, desktop and phone.
- "PINNED" cut under the filter box on the desktop shots was the harness: the list kept the scrollTop an
  earlier render left (60px at density 1, 48 at 0.72, 5 at 0.3), so the heading had scrolled up under the tray.
  The layout itself is fine (heading top equals the list top at scrollTop 0). The harness now starts each shot
  at the top of the list and asserts the heading is not above the list's top edge.
- The "before" shots in real3 use the same new harness, so they differ from real2 only by the scroll reset.
