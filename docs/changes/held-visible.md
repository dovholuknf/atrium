## Test plan

## @LETTER@. An fyi to the orchestrator is read, not parked

### @LETTER@1. An fyi arrives at turn end

1. Tag a card `atrium:orchestrator` and have it working a long turn.
2. From a worker, send it `atrium_say` with `kind: fyi`.

**Expected:** the answer says `queued` with `when: "done"`, not `held`. When the orchestrator's turn ends the text arrives
as `@worker says: "..."`. An idle orchestrator gets it at once.

### @LETTER@2. A done report reaches the launcher

1. A worker launched by that orchestrator calls `atrium_done`.

**Expected:** the answer says `launcher_told: true` and the report arrives typed at the end of the orchestrator's turn.

### @LETTER@3. Automatic notices stay held

1. Let a worker stop without reporting.

**Expected:** the silent stop notice is on the orchestrator's card and is read with `atrium_task` `notices`.