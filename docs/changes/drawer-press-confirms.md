## Test plan

## @LETTER@. The drawer check confirms its clicks

### @LETTER@1. A slow drawer does not fail the check

1. Serve the board with a short `pointer-events: none` window after `.fdrawer.open .fwrap`, for example an animation of
   330 ms, and run `node scripts/check-board-drawer.js`.

**Expected:** the stack, board and terminals presses in step 7 and the step 4 chip all pass. Before the change the three
step 7 lines failed with the sort still on `activity`.

### @LETTER@2. A button that never takes the click fails with its name

1. Make a drawer button ignore clicks and run the check.

**Expected:** after three tries the check fails with a line naming the selector and its class list, instead of failing
later on a state read.

### @LETTER@3. No delay

1. Run `node scripts/check-board-drawer.js` unmodified.

**Expected:** it ends with `the filter drawer holds.`
