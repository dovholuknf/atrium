Docs: none, a fix that removes a message the docs described

## Test plan

## @LETTER@. Lifting a deploy hold sends nothing

### @LETTER@1. Called off with working and idle cards

1. Have several idle cards and one working card in a room, set a deploy hold and let two of them make a tool call.
2. Call the hold off from the owner.

**Expected:** no card receives anything, nothing is queued for any card and nothing is typed into any terminal. No idle
card starts a turn. Their agents' held messages are delivered.

### @LETTER@2. The room restarts under a hold

1. Hold the room and restart it, on a new build and again on the same build.

**Expected:** the hold is lifted at startup, no card gets a restart wake or a `resume-continue` event, and a card's own
queued restart wake is still typed unchanged.

### @LETTER@3. What a refused call says

1. Under a hold, make a gated call from a held card.

**Expected:** the refusal says the room is being redeployed, that the call did not run and to run it again later. It
does not promise a message.
