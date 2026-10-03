# Review: u-row-flood 92b49ef2

Range `d739825c..92b49ef2`, one commit. These are two bugs clint raised on the desktop terminals list:
- the LAND badge is dropped from `ctxWarnMark`;
- the context line is now a thin strip along the row's bottom, in place of a fill behind the whole row.

It changes `css/cards.css`, `js/board.js`, the headless sections, the item doc and `docs/changes`.

Verdict: **OK** for room and hub. Two Lows can follow.

## How it was checked

- I read the CSS and JS diff and the headless asserts. Per the standing note, I read the board units and did not run
  them.
- `node --check` passes on board.js and test-board-headless.js at the tip.
- I looked at the after shot at 2000 in graphite, with four rows: plain, 10%, 80% and 130%. Every row's background
  and title match the plain row. The strip is neutral, amber, then danger, and it is inset clear of the left accent
  bar.

## Points

- **The badge.** The `landOver` branch and the `.chip.ctxland` rules are gone. The stack row and /m share
  `ctxWarnMark`, so they lose the badge too, which is what was asked.
  - The amber warn mark is unchanged.
  - `landOver` and `landTip` still feed `ctxLine`'s heat and its tooltip, and the details meter.
  - The comment now says why there is no badge: atrium clears an agent card at that line on its own.
- **The strip.** It is `position: absolute`, 3 px tall, 10 px in from each side and 3 px up from the bottom, with no
  fill behind the text.
  - The `.card:has(...)` rules for `isolation` and the danger border are removed. So a row past the land line looks
    like any other row, apart from its strip.
  - The heat steps are kept as solid fills. The land tick `s` stays visible on the hot strip.
- **The glow.** It runs once, on `.over`, and never loops. The existing `prefers-reduced-motion` rule at cards.css:807
  still turns it off.
- **The asserts.**
  - There is no LAND chip at 130%.
  - The strip is 2 to 4 px tall, at the bottom, and inset.
  - There are three distinct fills.
  - The row's background and border equal a plain row's.
  - The three mutants (the badge back, the flood back, the red border back) are each caught.

## Lows

- **L1: the tooltip's hit area is 11 px tall, and it sits on top of the row's bottom edge.** `::after` runs from 5 px
  above the strip to 3 px below it, and it takes the pointer. It is positioned, so it paints over the row's text in
  that band.
  - A click there still reaches the card, because it bubbles. But any control placed in the bottom 11 px, for example
    a chip on the path line at a narrow width, would get the tooltip in place of the click.
  - The old floor was 5 px. Keep it at 5 to 6 px, or put the hover on the row.
- **L2: the path line sits close to the strip.** At 2000 the path's descenders come within about 2 px of the strip
  (the "...ed-answers" row). One or two more pixels of bottom padding on rows that carry a line would separate them.

Atrium-Verdict: room-ok d739825c..92b49ef2
Atrium-Verdict: hub-ok d739825c..92b49ef2
Quality: a clean fix to what clint saw. The rows now carry the same weight, and each of the three regressions is held
by a mutant-checked assert.

## Re-read: 89fa4e32 (clint saw the strip drawn through the path line)

Range `a602d6ff..89fa4e32`, two commits. `8df3a802` takes L1 and L2. `89fa4e32` is the fix: every terminals row
reserves a 6px band under its content, and the strip sits in the bottom 5px of it.

Verdict: **OK** for room and hub. It is urgent, so I read it alone, without a fork.

- **The band is one rule, said three times, and the three agree.**
  - The general rule is `.card:has(> .peek-bar.ctxline)`: bottom padding is `--row-pad-b` (falling back to
    `--row-y-sm`) plus 6px.
  - The terminals list (`.term-list .card`), the phone list (5px + 6px = 11px) and `mini` (6px + 6px = 12px) each set
    `--row-pad-b` and write the same sum themselves.
  - So where both rules match, whichever wins by order gives the same value, and a row without a strip is the same
    height. A browser without `:has` still gets the band from the list rules.
- **The geometry.** The strip is 3px at `bottom: 2px`, so it covers 2-5px above the padding edge. Text ends
  `--row-pad-b` + 6px up, so the gap is at least the row's own padding plus 1px. The hit area (`top: -2px;
  bottom: -1px`) runs from 1px to 7px, which stays under the text for any padding of 1px or more. L1 (the 11px hit
  area) is closed by the same change.
- **The test.** `ctxLine` now walks every text run and chip in each row against the strip's box, and compares row
  heights with and without a strip. A long wrapping title and a long path are added at 2000 and 390.
  `node --check` passes. Per the standing note, I read the board units and did not run them.

Notes, none holds it:
- **N1: the cause is not reproduced.** On mock rows the old geometry missed the path box by tenths of a pixel. So the
  fix is a margin that is clearly larger, not a proven cause. Check one real hub row after it lands, as @ui asks,
  and include a row with many chips.
- **N2: one mutant survives.** Removing the band from both the terminal-list and the phone/mini rules together is not
  caught, because the old padding was nearly enough on mock rows. An assert that `padB` is at least the base padding
  plus 6px would catch it.
- **N3: `/m` was not checked.** It has its own styles.

Atrium-Verdict: room-ok a602d6ff..89fa4e32
Atrium-Verdict: hub-ok a602d6ff..89fa4e32

## Re-read: 6dcb0723 (restore the 3e9ca7a1 bar, rebased on 92ed8faa)

Range `92ed8faa..6dcb0723`, one commit. It drops the inset strip, the reserved band and the `::after` hit area. In
their place is a 4px bar, flush with the row's bottom edge and sides, inside the card (`.card` clips it with
`overflow: hidden`). It has a gradient for warm and hot, a one-off pulse, and the bar's own tooltip.

Verdict: **HOLD** on M1. It is a one-line fix, and I will re-read it at once.

## How it was checked

- I read the diff. `--warn-rgb` and `--danger-rgb` exist in every theme. `.card` clips, so the corners are cut by the
  row.
- **The PNGs:** I looked at after-2000-paper, after-390-graphite and before-2000-paper in `/tmp/u-row-flood/real/`,
  taken from the live copy on m1mini. The bar sits inside the bottom edge, nothing meets the path line, and the
  near-limit row is amber.
  - These shots are at the default density.
  - The 390 shots are the desktop board squeezed, not `/m`, as @ui says.

## M1: below normal density, the bar is taller than the padding it sits in

The bar is 4px at `bottom: 0`, inside a bottom padding of `--row-y-xs`, which is `5px × --uiscale × --density`. The
board offers density 0.72 (tight), 0.5 (tighter) and 0.3 (tightest). At those settings the padding is 3.6px, 2.5px
and 1.5px, so the bar runs 0.4px, 1.5px and 2.5px into the content box. That is the path line's last pixels. A
`--uiscale` below 0.8 does the same.

This is likely the cause the first fix could not reproduce. Mock rows run at density 1, where 5px covers a 3px strip
at `bottom: 3px` with nothing to spare. On a tight board, the same rows put the strip through the path line.

Fix, in `css/terminal.css`: `.term-list .card { padding-bottom: max(var(--row-y-xs), 5px); }`. The phone (5px) and
`mini` (6px) rules are fixed values and already clear it. Add a `ctxLine` case at density 0.3, and at a small
`--uiscale`, that runs the same text and chip clash walk.

Atrium-Verdict: hold 92ed8faa..6dcb0723
