## Test plan

## @LETTER@. The card peek tells the truth about status

### @LETTER@1. Needs you, your move, working

1. Hold the pointer on a card whose last turn ended with open questions.
2. Hold it on a card whose turn ended with none.
3. Hold it on a card that is mid turn.

**Expected:** the first line under the name reads "needs you" with the question count in the warn colour, then "your move, its turn ended, nothing asked" in teal with no needs-you block, then "working" with what it is doing. The grey line under holds only the path and model.

### @LETTER@2. An old recap

1. Open the peek on a card whose recap is older than its latest prompt.

**Expected:** a small line over the recap reads "said 9d ago, before its latest prompt". A recap newer than the prompt has no such line.

### @LETTER@3. Hover moves nothing

1. Open the peek on a card with a long open question, with a long path or word in it.
2. Move the pointer onto the question.

**Expected:** no horizontal scrollbar at any time, the question is only recoloured, and the peek keeps the same width and height. The needs-you block keeps its scrollbar gutter when it scrolls vertically.

### @LETTER@4. The limit line

1. Open the peek on a card with the hub limit at 160k and the land line at 200k, then at 200k and 200k.

**Expected:** "warns at 160k from hub, lands 200k", then "warns and lands at 200k from hub".
