## Test plan

## @LETTER@. The opencode export keeps what it printed

### @LETTER@1. Answer printed before the stop

1. On macOS or Linux run `go test -tags integration ./internal/daemon -run TestOpencodeExecIsBounded -count=10`.

**Expected:** every run passes, and each call returns within 3 seconds of the stop.
