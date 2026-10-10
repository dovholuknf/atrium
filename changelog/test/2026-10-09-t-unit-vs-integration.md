`go test ./...` now runs the unit tests only: 175 of the 4,045 tests, in about 6 seconds instead of 190. The other 3,870
(anything that opens a store, touches a disk, starts a process, a daemon or a room, uses a socket, or waits on a timer)
live in `*_integration_test.go` files under `//go:build integration` and run in CI with `bash scripts/test.sh all`. A
unit run fails any test over 10ms, with its name and time. Item t-unit-vs-integration.
