## Test plan

## @LETTER@. A board load reads settings once

### @LETTER@1. The stream opens late

1. Run `HEADLESS_UNITS=settingsOnce node scripts/test-board-headless.js`.
2. To force the timing, temporarily route `/v1/events` in the unit with a 400ms delay and wait 500ms before the count.

**Expected:** the unit passes. Without the wait for the live stream it fails with "read /v1/settings 2 times".
