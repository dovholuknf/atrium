## Test plan

## @LETTER@. Handoff when the cache goes cold

### @LETTER@1. Keep-alive stopped

1. Leave an idle Claude card with a context over 50k until keep-alive stops it at break-even, or turn its keep-alive off.
2. Wait until its cache is within five minutes of expiring.

**Expected:** the card runs the new-context cycle by itself, its timeline gains a "cache going cold" note, and
its next turn starts from the handoff. It does not cycle again until it has had a new turn.

### @LETTER@2. Switches

1. Untick "hand off and clear a card whose cache is going cold" in settings, or untick "hand off when its cache goes
   cold" in one card's menu.
2. Repeat the wait.

**Expected:** that card, or every card, is left alone. A card being refreshed by keep-alive is never handed off.

### @LETTER@3. One handoff per person's turn, and the notice

1. Let a card cold-hand-off. Wait out the continuation turn atrium types after the clear, and let it idle to expiry.
2. Type a turn yourself, or have a peer say something, and idle it to expiry again.

**Expected:** no second handoff after step 1, however large the continuation grew. After step 2 it may hand off once.
Each handoff raises a frost-blue "cache handoff" toast with a snowflake, flashes the card's row, adds a "cache handoff"
line to its timeline, and reaches the desktop notification when the board is not in front.
