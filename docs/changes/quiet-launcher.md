## Test plan

## @LETTER@. A launcher is not chattered at

### @LETTER@1. A long turn tells nobody but the board

1. From an orchestrator card, launch a worker and give it a turn longer than the long-turn setting, or lower the setting.

**Expected:** the worker's card wears the long-turn flag. The orchestrator's card shows no long-turn or long-tool
notice, held or typed. A silent stop still reaches it.

### @LETTER@2. A cross-room notice is held

1. On a room linked to a hub, launch a worker from an orchestrator card on another room and let it stop without a report.

**Expected:** the notice waits on the orchestrator's card and `atrium_task` with `notices` reads it. Nothing is typed into
its terminal.

### @LETTER@3. No held-notices chip

1. Open the board and the phone board with an orchestrator holding notices.

**Expected:** no mailbox icon on the row, the stack, the board card, the terminal bar or the peek.
