## Test plan

## @LETTER@. The link tests carry no unmarked flat waits

### @LETTER@1. The test waits check passes

1. Run `go run scripts/check-test-waits.go`.

**Expected:** it exits 0 with no count above baseline for `internal/link/lane_integration_test.go` or
`internal/link/zrok_test.go`.
