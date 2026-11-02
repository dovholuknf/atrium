## Test plan

## @LETTER@. Board units that failed in the CI shards wait on what they read

### @LETTER@1. The four units

1. Run `node scripts/test-board-headless.js parent`, `growlPhone`, `mGroupMenus` and `toastsTop` alone and beside
   other units.

**Expected:** each passes in both runs, with no timeout widened.
