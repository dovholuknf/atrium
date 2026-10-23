## Test plan

## @LETTER@. Slow integration tests set up with fewer git spawns

### @LETTER@1. Same assertions, less setup

1. Run `go test -tags integration -count=1 -run '^TestADueMarkCullsAcceptsAndArchives$' ./internal/daemon`, then
   `TestTheSweepReclaimsAnIdleLandedWorkerThatNeverSaidDone` and `TestKeepaliveEachRuleBlocksARefresh`.
2. Run `TestCloseDeletesAMergedHubBranchAndAsksAboutAnUnmergedOne` in `./internal/api`.

**Expected:** all four pass, each faster than before (about 6.6s, 10.7s, 2.7s and 8.5s earlier).
