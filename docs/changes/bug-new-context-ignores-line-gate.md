Docs: none, a fix to the compose box the docs already describe

## Test plan

## @LETTER@. The line moved into the new context box leaves the terminal

### @LETTER@1. A cold handoff over a part typed line

1. On a card with cold handoff on, type a few words into its input line and do not press Enter, as the handoff fires.

**Expected:** no "typing is held" sign while the words are on the line. The bar says "waiting for your line to
clear".

### @LETTER@2. The box takes the line

1. On a card whose board line the room believes is empty but the board still remembers, let a new context arm.

**Expected:** the "starting a new context" box opens with the text, and the terminal's input line is empty. Sending
or freeing the box puts the text in once.
