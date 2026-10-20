## Test plan

## @LETTER@. New context for a card with no shell

### @LETTER@1. Ready by tool

1. Launch a runner with Bash and PowerShell disallowed and push it past its context limit.
2. Read the limit prompt typed into it.
3. Have it call atrium_ready with a handoff.

**Expected:** the prompt names only atrium_ready. The handoff lands at the path on the card, /clear follows when the turn
ends and the wake reads the handoff back. With no cycle waiting, or an empty handoff, the tool is refused with a sentence.

### @LETTER@2. Stops asking

1. Start a new context on a card and never call ready.

**Expected:** after 3 asks the card reads "new context waiting: the session did not call ready after 3 asks" and nothing
more is typed. A late ready still completes the cycle.

### @LETTER@3. Ready from another shell

1. Run atrium ready --name with the card's alias, its id, then a wrong name.

**Expected:** the first two find the card. The wrong one is refused naming what it looked up.
