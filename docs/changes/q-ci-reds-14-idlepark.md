## Test plan

## @LETTER@. The idle park keeps status test is deterministic

### @LETTER@1. A park restores the status the card had

1. Run `go test -tags integration ./internal/daemon -run TestIdleParkKeepsStatus -count=20`.

**Expected:** every run passes and the card ends parked as done, never dead.
