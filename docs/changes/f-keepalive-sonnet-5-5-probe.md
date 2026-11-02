## Test plan

## @LETTER@. Keep-alive refreshes a Sonnet 5.5 card

### @LETTER@1. An idle Sonnet 5.5 card on the 1h cache

1. Launch a card on `claude-sonnet-5-5` with keep-alive on, give it one turn, and let it go idle.
2. Wait for the refresh before the cache expires.

**Expected:** the card's keep-alive ledger shows a warmed refresh that read nearly all of the context, not
`skipped: model claude-sonnet-5-5 keys its cache on effort`.
