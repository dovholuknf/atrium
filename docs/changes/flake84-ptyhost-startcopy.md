## Test plan

## @LETTER@. The pty host starts from a copy

### @LETTER@1. Start, dial and idle exit

1. Run `go test -tags integration -run TestStartRunsADetachedHostFromACopy ./internal/ptyhost/`.

**Expected:** it passes in about 2.5 s locally, with one `atrium-pty` copy made and the host gone shortly after the
client closes.
