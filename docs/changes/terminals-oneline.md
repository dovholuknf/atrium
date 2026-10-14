# Terminals rows are one line again

Rows in the Terminals list grew a second line: the chips (`resume`, the room chip, `new context failed`) dropped under
the title and a parked row, with its `resume` chip, came out taller than a running one.

## Cause

`unified-card` (be49b743) made the shared `.card .card-line` and `.card .card-line > .chips` in `css/cards.css` WRAP
so a busy board card grows a line instead of scrolling its chips. The Terminals list draws the same `.card` markup,
and its own rules only set `flex: none` on the chips, so with the title's content wider than the column the chips
wrapped. `parked-stays-in-terminals` (896ef11a) only made it show, by putting a `resume` chip on more rows.

## Decision

Fix it in the Terminals scope, `css/terminal.css`, and leave the board's cards wrapping: `.term-list .card .card-line`
and its `.chips` are `nowrap`. The title is already `min-width: 0` and truncates, so it gives way and the chips,
`resume` included, stay on the row. The resume chip stays, since the row is the only place a parked card says so. At
the narrowest widths a row with `new context failed` and a room chip leaves the title almost no room, which is the
old behaviour, and the chips never wrap or leave the card.

## The heldLink unit (not related)

`heldLink` failed when it ran after `peekLaunch` and passed alone. The unit called `openLaunch` without awaiting it.
`openLaunch` reads the runners first when the board has not (`await loadHarnesses()`) and then calls `clearResolved`,
which wiped the recognition the unit had just made. After `peekLaunch` the board was slower to hold them, so the wipe
landed late. The unit now awaits `openLaunch`. No product code changed, and it was not a regression from
`unified-card`.

Code: `css/terminal.css`. Tests: the `terminalsOneLine` headless unit (five rows, running and parked, at 320 and 270px:
one height, chips on the title's line, inside the card) and `heldLink`. Captures: `img/terminals-oneline-before-320.png`,
`img/terminals-oneline-after-320.png`, `img/terminals-oneline-after-270.png`.

## Test plan

## @LETTER@. Terminals rows are one line

1. Open the terminals tab with some running cards and some parked ones, and a room attached.
2. Narrow the list as far as it goes.

**Expected:** every row is the same height, with the title and path on the left and the chips on the right of one
line, at any width. No chip wraps under the title.
