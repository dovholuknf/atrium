## Test plan

## @LETTER@. A board load shares one settings read

### @LETTER@1. Slow load, one read

1. Open the board against a hub whose settings answer is held until the room list has arrived.
2. Release the answer and let the skin land.

**Expected:** the page made exactly one `/v1/settings` request, the skin is worn, and a read made afterwards reaches the
daemon again.
