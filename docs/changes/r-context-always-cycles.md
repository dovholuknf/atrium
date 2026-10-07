## Test plan

## @LETTER@. Every claude card cycles its context

### @LETTER@1. A resumed card

1. Resume a card whose conversation is past the hub's claude limit.
2. Wait one reaper tick.

**Expected:** the card shows a context size and the context cycle starts at the limit.

### @LETTER@2. A fixture card

1. Run a claude fixture such as main:dotfiles until its context passes the limit.

**Expected:** the context cycle starts on it, and it does not run on to auto-compact.
