## Test plan

## @LETTER@. Snowflake on a cold card

### @LETTER@1. Cold, warm and no cache

1. Have a keep-alive card whose cache has expired, one still warm, and one that has not made a turn yet.
2. Look at each on the board, the stack, the terminals list and its open card view.

**Expected:** only the expired card has a snowflake before its title, on all four. Hovering it reads the same words as the
keep-alive chip. The warm and new cards have none.

### @LETTER@2. Cooling and refreshing

1. Watch a warm card until its cache expires, then send it a turn.

**Expected:** the snowflake appears when the chip turns cold and goes when the cache is warm again, without a reload.

Screenshots: `docs/changes/img/f-cold-cache-snowflake-on-title/`.
