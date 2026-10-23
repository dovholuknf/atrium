Docs: none, test-only change

## Test plan

## @LETTER@. Fetch-time test stays fast

### @LETTER@1. Run the test

1. Run `bash scripts/test.sh full -run TestPRFetchSourceTimedAgainstALocalRemote ./internal/daemon`.

**Expected:** it passes in a few seconds and run.log holds `fetch: head fetched (` and `fetch: worktree made (`.
