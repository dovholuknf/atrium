## Test plan

## @LETTER@. A slow /v1/tasks answer does not break the 5s throttle

### @LETTER@1. Unit passes alone

1. Run `HEADLESS_UNITS=eventDriven node scripts/test-board-headless.js`.
2. It prints 4 or fewer `/v1/tasks` for 30s of task events without a row, with the mock answering each read 100ms late.
