Docs: none, a fix that makes the retry behave as the docs already say

## Test plan

## @LETTER@. Says held across a new-context cycle are typed promptly after the wake

### @LETTER@1. Held says arrive without a long wait

1. Make a card mid-turn and send it two `when: done` says and one immediate say.
2. Start a new context on it and let the cycle finish, then end the wake's turn.

**Expected:** every say, including the repeated immediate one, is typed once within a couple of seconds of the turn
ending, not after the widened retry interval.
