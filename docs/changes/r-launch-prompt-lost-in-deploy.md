## Test plan

## @LETTER@. A launch during a stop is refused

### @LETTER@1. Launch while the room winds down

1. Ask the room to stop with a wrap-up wait (`atrium stop`), and while it waits launch a card on it.
2. The launch fails with "this room is stopping: nothing was launched. retry the launch once it is back".
3. After the room is back, the same launch succeeds and the prompt is on the card.
