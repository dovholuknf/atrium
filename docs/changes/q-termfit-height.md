Docs: none, a test-only change

## Test plan

## @LETTER@. The termFitHeight unit waits for the rows the pane fits

### @LETTER@1. Attach, the hint bar and the window

1. Run `HEADLESS_UNITS=termFitHeight,bootClean node scripts/test-board-headless.js` several times.

**Expected:** both pass every time. Each check waits until the grid holds the rows the pane fits now, then the last row
ends at or above the hint bar's top, even when no resize followed the first fit after attach.
