## Test plan

## @LETTER@. Ctrl+enter in the open-questions flyout

### @LETTER@1. Steps while some are unanswered

1. Open a card with three open questions and open the flyout on the last one.
2. Answer only that one and press ctrl+enter (cmd+enter on a Mac).

**Expected:** the flyout stays open and moves to the first unanswered question. The hint reads "ctrl + enter for the
next one".

### @LETTER@2. Sends when all are answered

1. Answer the other two. Go to any question.
2. Read the hint, then press ctrl+enter.

**Expected:** the hint reads "ctrl + enter to send to the agent". The answers are sent as one message and the flyout
closes.
