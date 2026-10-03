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
