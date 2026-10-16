## Test plan

## @LETTER@. Items tab titles, steps and navigation

Pictures: `docs/changes/img/bug-items-tab-titles-nav-status/` (`before-*.png` and `after-*.png`, in the order 1 list, 2 list
scrolled, 3 detail, 4 back by the crumb, 5 back button). Headless: `HEADLESS_UNITS=itemsRealShape,bootClean node
scripts/test-board-headless.js`.

The cause: the hub's item rows name their backlog item by `work_id`, and the board looked for `backlog_id`. Nothing matched
the step engine's rows (`/_hub/work`), so every row kept the one `building` step the board makes up for an older hub, and
titled itself with the card or the uuid.

### @LETTER@1. Titles and steps

1. Open the items tab on a hub with several items at different steps.

**Expected:** every row reads its backlog item's title, with the backlog id under it. The line under the bar names the
step the item is really at (designed, approved, building, landed and so on), and rows differ.

### @LETTER@2. Navigation

1. Scroll the list down, open an item, and press `back` in the page's crumb.
2. Open an item again and press the browser's back button, then forward.

**Expected:** the page opens at its top. Back returns to the list at the scroll position it was left at, with the row
you opened focused. The browser's back closes the page and stays on the items tab, and forward reopens it. The list does
not flicker on the 5 second poll when nothing changed.
