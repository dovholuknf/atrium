## Test plan

## @LETTER@. A restart does not fire launch-idle notices

### @LETTER@1. Idle launched cards after a restart

1. Have an agent launch a worker and let the worker finish its first turn and sit idle.
2. Restart the room and wait two minutes.

**Expected:** the launcher is told nothing and nothing is typed into its terminal. The reopened card does not show a
"no activity since launch" escalation.

### @LETTER@2. A fresh launch that never starts

1. Launch a worker that stops at a folder trust prompt or never runs its first prompt.

**Expected:** the launcher still gets the "no activity since launch" notice after the grace.
