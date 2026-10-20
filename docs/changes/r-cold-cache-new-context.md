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
