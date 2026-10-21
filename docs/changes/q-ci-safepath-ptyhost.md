## Test plan

## @LETTER@. Denied ancestors and the detached host start

### @LETTER@1. A denied ancestor under a re-spelled root

1. Run `go test -tags integration -count=1 ./internal/safepath`.

**Expected:** all pass, including the test whose root resolves to another spelling, and a path outside the root is still
`ErrOutside`.

### @LETTER@2. A detached host that starts slowly

1. Run `go test -tags integration -count=1 -run TestStartRunsADetachedHostFromACopy ./internal/ptyhost`.

**Expected:** passes, and the host exits on its own once the client closes.
