Docs: none, a fix that makes worker cards and chips look the way the docs already describe

## Test plan

## @LETTER@. A worker never looks like its orchestrator

Before and after: `img/worker-card-colors-before.png`, `img/worker-card-colors-after.png`.

### @LETTER@1. Workers on the orchestrator's room and on another

1. Turn card colours on, and turn on the terminals list's colours for idle rows.
2. Give an orchestrator card its own theme. Have it launch one worker on its own room and one on another room.

**Expected:** the orchestrator wears its theme. Both workers are drawn the same way, in the skin's card colour, and
neither wears the orchestrator's theme. Each worker's room shows only in its room chip.

### @LETTER@2. A worker still takes its launcher's repo colour

1. Give the orchestrator's repo a colour in the card colours settings.
2. Launch a worker from it into a scratch folder.

**Expected:** the worker wears the repo colour, not the orchestrator's own theme.

### @LETTER@3. Chips read on every card

1. With two rooms attached, step through every skin, the light ones included (paper, linen, frost, daylight).
2. Look at the room chip and the `new` chip on a plain card and on a card in a dark theme.

**Expected:** the chip text is the card's title colour and reads clearly. The room's hue is on the chip's border, and
two rooms still read apart.
