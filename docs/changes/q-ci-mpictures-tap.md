## Test plan

## @LETTER@. The mPictures unit taps a settled thumbnail

### @LETTER@1. Tap after load

1. Run `HEADLESS_UNITS=bootClean,mPictures node scripts/test-board-headless.js`.
2. Run it again with `HEADLESS_SLOW=4`.

**Expected:** `mPictures ok` both times, with no `page.tap` timeout.
