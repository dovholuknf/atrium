Docs: none, a fix that changes no verb, flag, endpoint or setting

## Test plan

## @LETTER@. Join errors say what is wrong

### @LETTER@1. Used, expired and dead share

1. Join with a string twice.
2. Join with a string older than an hour.
3. Retire the hub's share, then join with a string minted before.

**Expected:** the first says used already, the second says expired, the third says the share in the join string is
gone. Each ends with `mint a new one with atrium rooms token <name> on the hub`.
