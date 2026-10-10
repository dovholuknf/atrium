## Test plan

## @LETTER@. Unit and integration tests run separately

### @LETTER@1. The fast loop is unit only

1. Run `bash scripts/test.sh` (the same as `go test ./...`).

**Expected:** about 175 tests pass in a few seconds. No package starts git, a shell, a daemon or a room.

### @LETTER@2. The unit budget is enforced

1. Add a test to any package that calls `time.Sleep(30 * time.Millisecond)` and run `bash scripts/test.sh unit ./internal/cardcolors/`.

**Expected:** the run fails with `OVER THE 10ms UNIT BUDGET` and the test's name and time. Delete the test again.

### @LETTER@3. Everything still runs in CI

1. Run `go test -tags integration -list '.*' ./... | grep -c '^Test'`.

**Expected:** 4,045 less the Example tests, the same as before the split. CI runs `bash scripts/test.sh all`. Run an
integration test by hand only when CI fails on it or when you add or change one:
`go test -count=1 -tags integration -run TestName ./internal/<package>/`.

### @LETTER@4. Both builds are vetted

1. Run `go vet ./...` and `go vet -tags integration ./...`.

**Expected:** both pass.

## The rule

A unit test runs in under 10ms, uses no external dependency (no database, no files on disk, no sockets, no
processes) and never sleeps or waits on a timer. Everything else is an integration test: put it in a
`*_integration_test.go` file whose first line is `//go:build integration`. When unsure, it is integration. Helpers
used by both kinds stay in untagged `_test.go` files; helpers only integration tests use go in the tagged file.
`TestMain` stays untagged. A file for one OS gets `//go:build integration && windows` (or the OS it is for), because
a `_windows_integration_test.go` name no longer implies the OS to Go.
