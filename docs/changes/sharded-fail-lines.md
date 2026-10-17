Docs: none, a test report fix.

## What and why

`scripts/test-board-sharded.js` printed only the first line of each failure under its unit, in all three of its
failure sections. A Playwright timeout puts the locator and the reason on the lines after the first, and the unit's
own `(at file:line)` after those, so a CI red read as `page.click: Timeout 30000ms exceeded.` and nothing else.

The runner now prints the whole message through one helper: the first line as before, up to six more lines indented
under it, ANSI colour stripped, every line cut at 300 characters, and a count of what was left out. The first line
keeps the four-space indent `board_report` in `scripts/ci.sh` reads for failing.tsv.

## Test plan

## @LETTER@. A sharded failure names its locator

### @LETTER@1. Force a timeout

1. In `changeReqSection`, change the click on `.cr-li[data-id="cr_8"]` to a selector the page lacks.
2. `node scripts/test-board-sharded.js --local --no-retry --units changeReq`.

**Expected:** under `FAILED (1):` and `changeReq`, the message is followed by `Call log:`, the
`waiting for locator(...)` line naming the selector, and the `(at changeReqSection (...test-board-headless.js:LINE))`
line. Revert the selector.
