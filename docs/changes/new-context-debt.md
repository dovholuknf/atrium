## Test plan

## @LETTER@. A new context creates no debt and no repeat

### @LETTER@1. No silent-stop notice after a cycle

1. Launch a worker from an orchestrator and have the launcher say something to it that it answers.
2. Cycle the worker's context with new context.
3. Let the fresh context end its turn without saying anything.

**Expected:** the launcher gets no "ended its turn without reporting" notice, and the worker's card shows no report owed.

### @LETTER@2. The continue prompt lists what was sent

1. Have a launched worker say something to its launcher after the launcher's last prompt.
2. Cycle the worker's context.

**Expected:** the wake prompt reads the handoff, then lists that say as already sent and delivered, with the
instruction not to resend it.

### @LETTER@3. A real silent stop still notifies

1. After the cycle, have the launcher prompt the worker and let it end its turn with nothing said.

**Expected:** the launcher gets one silent-stop notice.
