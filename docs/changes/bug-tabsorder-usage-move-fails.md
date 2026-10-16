# bug-tabsorder-usage-move-fails

The board was right and the test was stale. The Items tab (c3c1e121) sits ahead of usage in the default bar, so moving
usage up two places gives `stack,board,terms,chat,items,usage,pulls,history`. The check wanted the order without
`items`. Its expected order and the slice it compares now include `items`. No assertion was loosened.

## Test plan

- `HEADLESS_UNITS=tabsOrder node scripts/test-board-headless.js` passes.
