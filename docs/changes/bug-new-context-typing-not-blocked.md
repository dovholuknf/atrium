Docs: none, a fix that makes the gate match what it already says

## Test plan

## @LETTER@. Typing is blocked for the whole new context

### @LETTER@1. The first step holds the gate

1. Start a new context on a supervised card and open its terminal.
2. While the chip says "1/3: waiting for atrium ready", type in the terminal and press the ctrl-c button.

**Expected:** nothing reaches the runner's prompt. A do-not-enter sign over the terminal names the step and offers
"let me type", and the compose box opens with the key you typed.

### @LETTER@2. The way out

1. Click "let me type" on the sign.

**Expected:** the cycle ends, the sign goes, queued text is sent and the terminal takes keys again. The sign also
clears by itself when the cycle finishes or fails.
