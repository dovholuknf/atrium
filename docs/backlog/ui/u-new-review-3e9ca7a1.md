# Review: u-ctx-bar 3e9ca7a1 (a context line along each terminals row and board card)

Range cd07b86e..3e9ca7a1, read by @review on m1mini. This commit is unsigned, like every m1mini commit.

## What it does

- **ctxMeter** is factored out of peekBody, and the popover and the new row line both draw with it.
- **The line's heat** is tokens over the card's limit: teal below 60%, warn from 60%, danger from 85% or at the
  daemon's `warn`.
- **Past the limit:** the line pulses once (iteration count 1, none under reduced motion), and the terminals row now
  draws the limit chip.
- **A cycling card** shows its new-context chip in place of the pulse and the chip.
- **No line** for a card without context_size, or one that is done, dead or shelved.

## Checked

- **Placement:** `.card` is already `position: relative; overflow: hidden`, so the absolute 4px line sits on the
  bottom edge and is clipped to the corners. Terminal rows are `.card.tab` and get the same box.
- **Repaints:** they go through morphChildren, which keeps nodes, so the one-time pulse doesn't restart each time.
  It restarts only when the `over` class is newly put on.
- **The tooltip** goes through `esc()` into `data-tip` and is made of numbers only. There is no new inline handler
  and no innerHTML path for card text.
- **Syntax:** `node --check` passes on board.js, peek.js and terminal-list.js.
- **The headless unit ctxLineSection,** read rather than run (REVIEWER-NOTES), covers:
  - the four heat classes, the fill widths and the tick;
  - the bottom-edge geometry, the pulse once and only past the limit, and the tooltip;
  - the chip present and absent, and no line for none or shelved;
  - the cycling card;
  - the popover's meter byte-equal to ctxMeter's, and a fill colour in both skins.

  @ui reports ctxLine and bootClean pass alone. contextSize and peekEverywhere fail with the known 'rooms' error,
  which also fails on the untouched HEAD.

## Lows

- **L1:** the board-card path (cardHTML) has no case. Only terminal rows are asserted.
- **L2:** the line's percent is of the limit, while the existing mark's `c.pct` is of the window. A tooltip that
  says "of limit" would keep the two from reading as a disagreement.

Closed: none (first read) / Open: L1, L2

Verdict: HUB DEPLOY OK cd07b86e..3e9ca7a1.

Quality: small, with one drawing for both places and a thorough unit. Reduced motion and the cycling state are
handled.
