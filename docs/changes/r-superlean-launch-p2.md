## Test plan

## @LETTER@. Superlean from the callers

### @LETTER@1. The hub's atrium_launch

1. Call `atrium_launch` with `superlean: true` on a claude runner. The card's tags hold `atrium:superlean` and `atrium:lean`, and the answer carries no warning.
2. Call it with `superlean: true` and `lean: false`. It is refused and no card appears.
3. Call it with neither. The card is lean, as before, and has no `atrium:superlean`.
4. Against a room older than the field, `superlean: true` starts a lean worker and the answer says `superlean was NOT applied`.

### @LETTER@2. The stdio server

1. From a session on a room, call `atrium_launch` with `superlean: true`. The card is superlean.
2. Do the same with `room` naming another room. The card on that room is superlean.

### @LETTER@3. The command

1. `atrium launch --lean --item <id>` starts a lean card.
2. `atrium launch --superlean --item <id>` starts a superlean card, with `atrium:lean` too.
3. `atrium launch --superlean --runner codex --item <id>` is refused by the room.
