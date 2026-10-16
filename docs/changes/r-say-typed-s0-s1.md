Docs: docs/runtime/agent-messaging.md, docs/terminal/queued-box-findings.md

## Test plan

## @LETTER@. A mid-turn Claude runner is told by hook, never typed into

### @LETTER@1. Operator channel mid-turn

1. Start a Claude runner and give it a long running tool call, so it is mid-turn.
2. From the board, say something to it.

**Expected:** nothing appears in its terminal box, the answer is `queued`, and the text arrives when the tool returns.

### @LETTER@2. Idle runner

1. Let the runner finish its turn and sit idle, then say something to it.

**Expected:** the text is typed and the answer is `terminal` or `delivered`.

### @LETTER@3. Escalated done and retry

1. Mid-turn, send a message that escalates, and one that retries.

**Expected:** neither types into the runner while it is mid-turn, and both answer `queued`.

### @LETTER@4. A script with no sender

1. Run `atrium tell` with no `from` against a mid-turn Claude runner.

**Expected:** it now waits for the hook to carry it instead of typing at once. A codex runner is unchanged.
