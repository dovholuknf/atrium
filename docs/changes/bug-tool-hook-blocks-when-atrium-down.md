## Test plan

## @LETTER@. Hooks fail open fast

### @LETTER@1. Room stopped

1. Stop the room with `atrium stop`.
2. Run a tool call in a session with atrium hooks installed.
3. The tool runs at once and no hook holds the session.

### @LETTER@2. Room frozen

1. Replace the agent listener with one that accepts and never answers.
2. Run a tool call. It runs within about two seconds.
