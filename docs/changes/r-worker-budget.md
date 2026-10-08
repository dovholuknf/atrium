## Test plan

## @LETTER@. Worker defaults

Settings, under the context limit: "default worker model" and "worker budget, in dollars". Both are empty on a fresh
install. The hub owns them and hands them to every room, like the context limit.

### @LETTER@1. Both are off by default

1. Open settings on a board that never set them.

**Expected:** the model box is empty and the budget box is empty. A worker launched with no model starts on the
runner's own default, and no worker is ever reported as over budget.

### @LETTER@2. A default model reaches a worker that named none

1. Type `sonnet` in "default worker model".
2. From an agent, launch a worker with no model.
3. From the board's own dialog, start a card with no model.
4. From an agent, launch a worker that names `opus`.

**Expected:** the worker in step 2 runs on Sonnet. The card in step 3 runs on the runner's default. The worker in step
4 runs on Opus. A resumed worker is left on the model it had. Emptying the box turns it off again.

### @LETTER@3. A worker past its budget is reported once

1. Type `5` in "worker budget, in dollars".
2. Let an agent-launched worker spend more than $5 at list price, as the usage tab prices it.

**Expected:** the worker's launcher is told once, as a held notice if it holds them, saying what was spent and that it
is a report and not an action. The worker's peek shows a `budget` line. Later turns do not repeat the notice. The worker
keeps running. A card you started yourself never shows one. Emptying the box or typing `0` clears the line.

### @LETTER@4. Every room gets it

1. Set both with the board scoped to ALL, then scope to one room and read settings.
2. Restart a room.

**Expected:** every room shows the same values, and a room that was down when they were set shows them once it
attaches.
