## Test plan

## @LETTER@. One board shard per core in CI

### @LETTER@1. The shard count follows the cores

1. Run `bash -n scripts/ci.sh`.
2. Read the shard line in the `board` and `board-visual` steps of `scripts/ci.sh`.

**Expected:** the script parses, and both steps set `shards` to the core count, never below 2 or above 16.
