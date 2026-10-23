Docs: none, test-only change

## Test plan

## @LETTER@. gitsync tests spawn fewer git processes

### @LETTER@1. The gitsync package still passes

1. Run `bash scripts/test.sh full ./internal/gitsync`.

**Expected:** every test passes and the ten slowest tests start fewer git processes than before.
