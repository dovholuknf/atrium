## Test plan

## @LETTER@. The terminal refits when its cell height changes

### @LETTER@1. The last row reaches the hint bar after attach

1. Run `HEADLESS_UNITS=termFitHeight,bootClean node scripts/test-board-headless.js`.

**Expected:** both pass. After attach the last row ends at or above the hint bar's top, within one row of it, even when
the font is measured after the first fit.
