## Test plan

## @LETTER@. The forge refresh test does not race the clock

### @LETTER@1. Run the test repeatedly

1. Run `go test -tags integration ./internal/gitsync/ -run TestMainIsRefreshedAtMostOncePerFreshFor -count=20`.

**Expected:** ok. The test freezes the store's forge clock, so a slow machine cannot expire forgeFreshFor between its
two fetches. Production uses time.Now as before.

Proof: `-count=20` passed. The full package run 3 times failed each time, only on three cherry-pick tests that need
~/.ssh/id_ed25519_sign.pub, which this machine lacks (TestEpochMoveKeepsWhatLandedAfterClintFetched,
TestEpochMoveToMainWhenEverythingIsAlreadyThere, TestItemFactsSeeThroughACherryPick). The target test did not fail.
