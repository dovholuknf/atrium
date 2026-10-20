## Test plan

## @LETTER@. The deployready tests stay cheap

### @LETTER@1. Run the package

1. Run `go test -tags integration,git -count=1 ./internal/deployready/`.

**Expected:** every test passes and none of the nine named in the changelog entry needs a retry.
