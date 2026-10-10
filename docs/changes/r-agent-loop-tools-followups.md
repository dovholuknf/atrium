## Test plan

## @LETTER@. A worker's ending reaches its launcher

Needs two rooms on one hub, or a launcher tagged `atrium:hold-notices`.

### @LETTER@1. Across rooms

1. From a session on room A, launch a worker on room B.
2. Have the worker call `atrium_done` with a commit, then do the same with `atrium_blocked` on a second worker.

**Expected:** the worker is told `launcher_told: true`, and the launcher on room A reads the report.

### @LETTER@2. Held under atrium:hold-notices

1. Tag the launcher `atrium:hold-notices` and have a worker call `atrium_done`.

**Expected:** the worker is told `launcher_told: held`. Nothing is typed or queued for the launcher, the board rings,
and `atrium_task` with `notices: true` shows the report.
