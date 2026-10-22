## Test plan

## @LETTER@. The phone resume bound test does not race its own timer

### @LETTER@1. Opening state is read in the same turn as the resume

1. Run `HEADLESS_UNITS=mResume node scripts/test-board-headless.js`.

**Expected:** it passes, and the resume is checked as opening in the same page turn that starts it, so a loaded runner
cannot let the 400ms bound expire before the check.
