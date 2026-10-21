## Test plan

## @LETTER@. Answering a question from the tray

### @LETTER@1. Answer from a question row

1. Let a card ask two questions, open the bell, and click the first question under its "asked 2 questions" row.
2. Answer it, press next, answer the second, and press "send to agent now".

**Expected:** the tray stays open, the answer box opens beside it, and one message with both answers reaches the card.
The row then reads "answered" and offers no questions. Clicking elsewhere on the row still opens the card.

### @LETTER@2. Answered elsewhere

1. With the tray open on a question row, answer that card's question from its hover panel.

**Expected:** within a poll the tray row reads "answered".

### @LETTER@3. Headless

1. Run `node scripts/test-tray-answers.js`. Set `TRAY_SHOTS=dir` for PNGs.

**Expected:** passes. The control is the peek's own (`peekQuestionsHtml`), so the draft and the send are the peek's.
