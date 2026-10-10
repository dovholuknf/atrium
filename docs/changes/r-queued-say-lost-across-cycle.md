## Test plan

## @LETTER@. A say is not lost to a new context

### @LETTER@1. Says sent just before a cycle

1. On a card that is mid-turn, send it two `when: done` says and one immediate say from another card.
2. Press New context on it and let the cycle finish.

**Expected:** after the wake prompt, each of the three arrives once more, each starting with a line saying it is a
repeat. Nothing is typed between `/clear` and the wake prompt.
