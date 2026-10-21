## Test plan

## @LETTER@. The new-context gate blocks typing and keeps one layout

### @LETTER@1. Typing while the gate is up

1. Start a new context on a card and attach its terminal while a step is running.
2. Type into the terminal's prompt.

**Expected:** nothing reaches the prompt. The first key opens the compose box seeded with it. After "let me type" the
terminal takes keys again.

### @LETTER@2. The modal on each step, at desktop and phone width

1. Watch the modal through steps 1/3, 2/3 and 3/3, then repeat at about 390px wide.

**Expected:** three lines (the hold, the step, the button) at one width on every step, no overflow on the phone. The
bottom strip says the step and has no "let me type".
