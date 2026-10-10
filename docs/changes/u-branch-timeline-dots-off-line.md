## Test plan

## @LETTER@. Branch timeline rings sit on the line

### @LETTER@1. Rings are centred on the line

1. Open the repos tab in the ledger view and pick a repo with several branches.
2. Look at the Branch timeline, in a light skin and in a dark skin.
3. Zoom the page to 150 percent and look again.

**Expected:** every ring is centred on the vertical line, the line is one colour from the day heading to the last
node, and the check `hubReposTimelineDots` in `scripts/test-board-headless.js` passes.

Before and after: `docs/changes/img/timeline-dots-before.png` and `docs/changes/img/timeline-dots-after.png`.
