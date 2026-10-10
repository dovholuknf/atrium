## Test plan

## @LETTER@. The details popover lays its panels side by side when stacked is too tall

### @LETTER@1. A short window

1. Open the terminals view, attach a card and shrink the window until the `details` popover, stacked, would run off
   the top.
2. Click `details` at the lower right.

**Expected:** the card panel sits to the left of DEBUG, growing leftward from the button. The recap is visible and
neither panel runs off the top. A panel still too tall scrolls inside itself.

### @LETTER@2. A tall window, and resizing

1. Make the window tall and open `details`.
2. Drag the window shorter and taller with the popover open.

**Expected:** the panels stack when they fit and go side by side when they do not, re-deciding as you drag. The `details`
button does not move. `node scripts/test-board-headless.js detailsSide` checks this, and `detailsHover` still checks the
pill's label.

Pictures: `img/details-side-by-side-before.png` and `img/details-side-by-side-after.png`.
