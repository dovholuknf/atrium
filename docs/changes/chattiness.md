## Test plan

## @LETTER@. Chattiness tool

### @LETTER@1. The fixture test passes

1. Run `scripts/chattiness/test.sh`.

**Expected:** it prints `chattiness: ok`.

### @LETTER@2. A window gives a summary

1. Run `scripts/chattiness/chattiness.sh --since <yesterday 22:00 with offset> --tsv /dev/null`.

**Expected:** a markdown summary with totals, a verdict per source and the worst sessions, and no jq error.

### @LETTER@3. An unreachable room is named

1. Run `scripts/chattiness/collect.sh --rooms local,nosuchhost`.

**Expected:** stderr gives the ssh error for `nosuchhost` and the summary lists it under rooms not covered.
