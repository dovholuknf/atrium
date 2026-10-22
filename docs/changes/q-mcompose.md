## Test plan

## @LETTER@. The mobile composer unit waits for the send

### @LETTER@1. Multi-line send

1. Run `HEADLESS_UNITS=mCompose node scripts/test-board-headless.js` several times, on a slow machine if you can.

**Expected:** every run passes, including the claude multi-line and the gemini joined-with-spaces checks.
