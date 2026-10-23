## Test plan

## @LETTER@. The slow board units still assert what they did

### @LETTER@1. Each unit passes alone

1. On a machine with playwright, run each of `phoneBoot`, `changeReq`, `mChangeReq`, `mPull`, `phoneView`,
   `phoneRedirect`, `tooltip` and `mDocs` with `HEADLESS_UNITS=<name> node scripts/test-board-headless.js`.

**Expected:** each prints `<name> ok` and a TIMING line below the figures in `REPORT.md`'s "before" column.

### @LETTER@2. The waits are conditions

1. Read the diff of `scripts/test-board-headless.js`.

**Expected:** no wait was lengthened. A wait that asserts "nothing happened" still has a window (the fake clock for
tooltip, a real sleep for the bounce-back in phoneRedirect), and the rest wait on a response, an element or frames.
