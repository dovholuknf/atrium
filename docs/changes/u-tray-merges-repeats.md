## Test plan

## @LETTER@. The tray merges repeats and dims agent-launched cards

### @LETTER@1. Repeats

1. Let a card raise the same alert four times with a changing minute count, then open the bell.

**Expected:** one row with the latest text and time and a count of 4, moved to the top by each new one.

### @LETTER@2. Agent-launched cards

1. Let a card another session launched go ready, then open the bell and press "all" in the header.

**Expected:** the row is dimmed, and "mine only" hides it and is kept across a reload. A permission or a question is never dimmed.
