## Test plan

## @LETTER@. Restart reopens terminals quickly

### @LETTER@1. Many cards

1. With a dozen or more cards that had runners, run `atrium stop` and start the room again.
2. Watch the log for `reopening N terminal(s)` and `reopened N of N`.

**Expected:** the two lines are seconds apart, not 0.4s per card, and every card has its terminal back.
