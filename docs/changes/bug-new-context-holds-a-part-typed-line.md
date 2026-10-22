Docs: none, a fix that makes the hold match what the docs already say about the line gate

## Test plan

## @LETTER@. A new context waits for a part typed line before it holds input

### @LETTER@1. The board button over a part typed line

1. On a card atrium owns the terminal of, type a few words into its input line and do not press Enter.
2. Press new context on the card.

**Expected:** no "typing is held" sign. The bar under the terminal says "new context 1/3: waiting for your line to
clear", and keys still reach the terminal. No limit prompt is typed.

### @LETTER@2. Clearing the line arms it

1. Delete what you typed and take your hands off the keyboard for a few seconds.

**Expected:** "typing is held for the new context" comes up, and the limit prompt is typed. The room log says
"the input line is clear, holding input".

### @LETTER@3. A peer message is not held before the line clears

1. Repeat 1, then have another card send this one a message before you clear the line.

**Expected:** the message waits behind your line as an ordinary held message, and the card does not say it is
queued for the new context.
