## Test plan

## @LETTER@. The cycle and the human's line

### @LETTER@1. Typing when the limit is reached

1. Put a card past its context limit while its prompt holds half a line, or while you are typing.

**Expected:** no cycle starts, no do-not-enter sign shows. Once the line is cleared or sent and the keyboard has been
quiet for a couple of seconds the cycle starts.

### @LETTER@2. A cycle that starts under your hands

1. Start a new context on the open card, having typed a few characters just before.
2. Keep typing while it runs, then let it finish.

**Expected:** the sign shows at once, no key reaches the prompt, the compose box holds what you had typed, and when the
cycle ends that text is on the new prompt's line, not submitted.

Screenshots: `docs/changes/img/bug-new-context-cycle-over-human-typing/`.
