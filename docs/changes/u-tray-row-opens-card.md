## Test plan

## @LETTER@. A tray row opens its card

### @LETTER@1. Clicking a row

1. Let a card on another room ask a question, open the bell, and click the row's title.
2. Click the row's copy icon, then "remind me".

**Expected:** step 1 closes the tray and opens that card's terminal. Step 2 copies and opens the remind menu and does not
navigate. Before, a row made from a growler carried no card, so it was not clickable.

### @LETTER@2. Headless

1. Run `HEADLESS_UNITS=growlTrayRow node scripts/test-board-headless.js`.

**Expected:** passes. Before the change the row had `data-task` null and a click landed nowhere.
