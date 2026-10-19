Docs: docs/runtime/card-home-design.md sections 2 and 10.3

## Test plan

## @LETTER@. Atrium's own files in the home

### @LETTER@1. Says and handoffs

1. Launch a card with `home` set, then say a multi-line message to it from its launcher.
2. Run new context on the card.

**Expected:** the say is a file under `<home>/.atrium/says/` and the handoff is `<home>/.atrium/handoff.md`. A card with
no home still uses the temp folder places.

### @LETTER@2. The wording

1. Open a room's card with the new-card form and read the throwaway help, then look at the room tile.

**Expected:** the help says a promoted throwaway stays where it is when the room has a card root. The tile shows free
space once, on the disk line.
