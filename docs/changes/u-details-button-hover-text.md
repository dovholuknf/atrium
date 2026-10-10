## Test plan

## @LETTER@. The details pill keeps its label on hover

### @LETTER@1. Hover on a plain card

1. Open the terminals view and attach a card.
2. Move the pointer over the `details` pill at the bottom right of the card.

**Expected:** the label stays readable, the outline lights up and there is no fill behind it.

### @LETTER@2. Hover on a themed card and in other skins

1. Give the card a light terminal theme, then a dark one.
2. Switch the board skin between a light skin, a dark skin and the website skin, and hover the pill each time.

**Expected:** the label reads against the card every time. `node scripts/test-board-headless.js detailsHover` checks
this in every skin on a plain, a dark and a light card.
