## Test plan

## @LETTER@. Cards stay out of the agent view

### @LETTER@1. Left arrow on an empty prompt

1. Launch a claude card, lean or not, and wait for the empty prompt.
2. Press the left arrow.

**Expected:** nothing happens. The conversation is not backgrounded into the agent view.

### @LETTER@2. The launch carries the keys

1. Look at the claude command line of a running card.

**Expected:** its `--settings` JSON holds `"disableAgentView":true` and `"leftArrowOpensAgents":false`, next to any hooks.
