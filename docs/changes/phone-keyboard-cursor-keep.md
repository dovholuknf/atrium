Docs: none, a fix that keeps the phone cursor row above the key bar after the keyboard resizes the layout

## Test plan

## @LETTER@. The cursor row stays above the key bar when the keyboard opens

### @LETTER@1. The keyboard opens while a cursor follow is already queued

1. Open a card's terminal on a phone, with the cursor on the last row.
2. Open the on-screen keyboard, in the phone view and in full screen.

**Expected:** the cursor row sits above the key bar, however busy the page was when the keyboard came up.
`HEADLESS_UNITS=phoneKeyboard node scripts/test-board-headless.js` queues a follow just before each keyboard resize.
