## Test plan

## @LETTER@. The board idles

### @LETTER@1. Nothing loops while idle

1. Open the board's Terminals tab with working and waiting cards and leave it alone for 15 seconds.

**Expected:** the waiting dot, held chip, permission count in the tab bar, hot room tile and reconnecting dot each pulse a few times and then are still. Their colours and sizes are unchanged.

### @LETTER@2. Top nav layout

1. Resize the window and open and close the filters drawer.

**Expected:** the tabs and the chips lay out correctly after each change, with one layout per frame (Performance panel shows no repeated forced reflows from `topnav.js`).

### @LETTER@3. Failed probes are not repeated

1. Attach to a card with no findings directory and watch the network panel.

**Expected:** `files/list?path=findings` is asked once for that card, not on every attach. After a failed `/v1/usage/turn-limits` the next ask comes ten minutes later.

### @LETTER@4. Headless units

1. Run `node scripts/test-board-headless.js noResumeButton`.

**Expected:** it starts and the unit passes.
