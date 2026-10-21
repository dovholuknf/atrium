## Test plan

## @LETTER@. A burst of task publishes folds into one event per card

### @LETTER@1. Coalescing under a slow store

1. Run `go test -tags integration -count=20 -run TestPublishTaskCoalescesABurstToTheLatestState ./internal/api`.

**Expected:** every run passes with exactly two events, and the card's event carries `"asks_open":10`.
