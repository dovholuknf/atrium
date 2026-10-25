Docs: docs/fabric/room-over-zrok.md's table names the new backoff line

## Test plan

## @LETTER@. The backoff line names the cause

### @LETTER@1. Hub stopped

1. Attach a room over zrok and stop the hub.

**Expected:** the room's lines say `not dialling that zrok share again for Ns. the last dial: the hub is not answering
on its share`, not that the controller is down.
