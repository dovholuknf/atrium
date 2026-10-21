## Test plan

## @LETTER@. The phone and title checks hold

### @LETTER@1. Static checks

1. Run `bash scripts/check-titles.sh` and `node scripts/check-phone.js` through `bash scripts/check-board.sh`.

**Expected:** no native title tooltips, and "the phone invariants hold."

### @LETTER@2. New-context line tooltip

1. Start a new context on a card and hover the do-not-enter line over its terminal.

**Expected:** the themed tooltip shows the full step text, and no native title appears.
