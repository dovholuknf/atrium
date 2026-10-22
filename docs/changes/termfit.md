## Test plan

## @LETTER@. The termFitHeight unit waits for the refit

### @LETTER@1. The hint bar and window refit

1. Run `HEADLESS_UNITS=termFitHeight,bootClean node scripts/test-board-headless.js`.

**Expected:** both pass. Each check waits until the board's refit has consumed the pane's current size, then the last
row ends at or above the hint bar's top.
