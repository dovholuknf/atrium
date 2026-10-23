## Test plan

## @LETTER@. The CI checks for waits and globals are clean

### @LETTER@1. Both checks pass

1. Run `go run scripts/check-test-waits.go` and `go run scripts/check-test-globals.go`.

**Expected:** both exit 0 with no FAILED line.
