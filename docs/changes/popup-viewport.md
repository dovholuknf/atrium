## Test plan

## @LETTER@. Popups stay inside the window

One helper, `js/popfit.js` (`popFit` for fixed popups, `popFly` for ones drawn inside their owner), places the card menu
and its flyouts, tooltips, the card details popover, the room picker, the answers flyout, the tabs overflow menu, the
phone key label and the held-line menu. The phone page `/m` has no scripted popups: its card picker and sheets are
fixed to the screen by CSS and already scroll. Before and after pictures of the group flyout in a short window are in
`docs/changes/popup-viewport-shots/`.

### @LETTER@1. A flyout in a short window

1. Make the browser window short, about 420 pixels tall.
2. Right click a card, point at "add to group".

**Expected:** the whole flyout is on screen. It sits as high as it needs to, and when the groups are more than the
window can show it scrolls inside itself.

### @LETTER@2. Near the right and bottom edges

1. Right click a card near the right edge, then near the bottom right corner.
2. Hover a `?` bubble near the bottom of the window, and a card to open its details.

**Expected:** the menu, its flyout, the bubble and the details popover each flip left or up rather than leave the
window, and none is cut off on any side.

### @LETTER@3. Automated

`HEADLESS_UNITS=popupFit node scripts/test-board-headless.js` opens each popup kind at the corners and edge middles of
a 420 by 300 window and fails when one leaves it. `POPUP_SHOTS=<dir>` writes the group flyout picture, with
`POPUP_BEFORE=1` for the old placement.
