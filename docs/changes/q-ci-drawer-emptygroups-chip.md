## Test plan

## @LETTER@. The drawer check and the empty groups chip

### @LETTER@1. The drawer check passes

1. Run `NODE_PATH=<dir with playwright> node scripts/check-board-drawer.js`.

**Expected:** every check is ok at 1280 and 390, including the default chips of each tab (the empty groups chip is on)
and the kept terminals state in step 7.
