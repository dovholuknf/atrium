Docs: none, a test harness fix.

## What and why

A Playwright timeout in a headless board unit said `page.click: Timeout 30000ms exceeded` and, through `threwAt`, at
best the unit function. Its call log named the locator only on later lines, and in a sharded run those were not
printed. A unit of two hundred lines with twenty clicks could not say which one hung.

`scripts/lib/browser.js` now wraps every page the suite opens (`withTrace`, applied over `withClock` at launch):
goto, click, fill, type, press, hover, check, selectOption, the waitFor family, and the same actions on a
`page.locator(...)`. Each wrapper reads its call site in the suite as the action starts and, when the action rejects,
puts `click("selector") at unitFn (file:line:col)` on the error as `calledAt`. `threwAt` prefers that. The three
places that report a throw go through `threwMessage`, which puts the place on the first line of the message, the one
failing.tsv keeps, with Playwright's call log after it and its colour codes stripped.

`HEADLESS_TRACE=1` prints `TRACE <unit> <action> at <call site>` as each action starts, so a hang is visible as the
last line printed rather than found after a timeout.

## Test plan

## @LETTER@. A timeout names its call and line

### @LETTER@1. Force one

1. In `changeReqSection`, change the click on `.cr-li[data-id="cr_8"]` to a selector the page lacks.
2. `HEADLESS_TRACE=1 HEADLESS_UNITS=changeReq node scripts/test-board-headless.js`.

**Expected:** `TRACE changeReq goto(...)`, the fixture's `waitForFunction` lines, then
`TRACE changeReq click(".cr-li[data-id=\"cr_nope\"]") at changeReqSection (...test-board-headless.js:LINE:COL)` as
the last trace line. The FAIL line reads `page.click: Timeout 30000ms exceeded. (click(".cr-li[data-id=\"cr_nope\"]")
at changeReqSection (...:LINE:COL))` and the call log follows it. Revert the selector.

### @LETTER@2. Through the shards

1. The same change, then `node scripts/test-board-sharded.js --local --no-retry --units changeReq`.

**Expected:** under `FAILED (1):` the same first line, with the call log indented under it.
