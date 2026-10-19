A deploy hold that is called off, runs out or is lifted from the board now tells only the cards whose tool call it
refused, and queues the line for their next call instead of typing it, so dormant sessions are no longer woken.
`deploy-batch.ps1` asks whether the room will refuse the stop before it sets any hold, so a deploy that is going to
refuse never holds the room (bug-deploy-hold-wakes-dormant-cards).
