## Test plan

## @LETTER@. The flaky suite holds only tests that failed lately

### @LETTER@1. The manifest

1. Run `node scripts/flaky-suite.js check` and `node scripts/check-test-buckets.js`.

**Expected:** both pass, and `scripts/flaky-suite.json` lists 14 entries.
