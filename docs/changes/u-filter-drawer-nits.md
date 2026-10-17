# u-filter-drawer-nits

Three small follow-ups from the review of the filter drawer fix.

- **Going to a card turns on the chip that hides it.** `revealCard` used to open folds only, so a switcher, toast or
  card-link jump to a card hidden by a chip landed on nothing. It now also turns on, in the view on screen, that card's
  column chip (board or stack) and the `subagents` chip when the card is rolled into its parent. Every other chip and the
  search are left alone. The terminals list is untouched, its headings carry the bridge (`js/reveal.js`, `revealChips`).
- **The attached card shows even when its column chip is off,** so the column count and the `filters N` count do not
  reflect it.
- Headless: the gear unit no longer asserts the gone `hide` controls twice.

## Test plan

1. On the board turn off the column a card is in, then jump to the card from the switcher. The column comes back, the
   card is drawn, and a second column you had turned off stays off.
2. On the stack turn off the card's column pill and jump to it the same way. Its pill turns on and the others are as
   they were.
3. Headless unit `revealChip` covers both.
