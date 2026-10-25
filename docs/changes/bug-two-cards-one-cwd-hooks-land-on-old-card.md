Docs: none, a fix to which card a hook lands on

## Test plan

## @LETTER@. A relaunch in a checkout with a card keeps its own hooks

### @LETTER@1. Two cards, one directory

1. Launch a worker in a checkout. Let it finish.
2. Launch a second worker in the same checkout and let it make a tool call that asks permission.

**Expected:** the permission request and the tool activity show on the second card. The first card shows neither.
