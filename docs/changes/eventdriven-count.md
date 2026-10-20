## Test plan

## @LETTER@. The whole-row count ignores a trailing pull from the partial events

### @LETTER@1. Unit passes alone

1. Run `HEADLESS_UNITS=eventDriven node scripts/test-board-headless.js`.
2. It prints 1 `/v1/tasks` for 30s of task events with `row: 1`.

### @LETTER@2. Unit passes when the mock answers slowly

1. Temporarily set `mock.tasksDelay` to 1000 instead of 100 in `eventDrivenSection`, then run the same command.
2. It still prints 1 `/v1/tasks` for the whole-row window. Before the fix it printed 2 and failed.
