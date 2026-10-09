## Test plan

## @LETTER@. The card peek is small

### @LETTER@1. One question and a recap

1. Hold the pointer for a second on a card that has one open question and a recap.

**Expected:** the popover holds the name, repo:branch with status and model, NEEDS YOU with the question, the recap in at most four lines with "more" under it when it runs longer, the context bar with one caption line, and a row reading open, say, exit, restart. It is no taller than 420px at the default UI scale. There is no card block, launch command, usage grid, cache paragraph or footer.

### @LETTER@1b. The documents line

1. Hold the pointer on a card that published documents, on a hub board.

**Expected:** "published N documents" sits under the context bar, every time, including when the usage read lands after the document count was asked. The popover is no wider than 420px and no taller than 460px, and the needs-you block scrolls inside itself past a few lines.

### @LETTER@2. The recap and the actions

1. Click "more" under the recap, then "less".
2. Click each of open, say, exit and restart on a card atrium runs.

**Expected:** the recap opens in full and folds back. Open attaches the terminal, say asks for a message and queues it, exit and restart each ask before they act. A card atrium does not run has only open and say.

### @LETTER@3. A short window

1. Make the browser window about 260px tall and open the peek on the same card.

**Expected:** the peek stays inside the window and scrolls inside itself.
