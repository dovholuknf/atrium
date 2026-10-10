## Test plan

## @LETTER@. The integration suite is fast and green

### @LETTER@1. The link package

1. Run `go test -count=1 -tags integration ./internal/link/`.

**Expected:** it passes in under 3 minutes. The "store is busy" lines in the log come from a test that makes its own
store refuse twice, and are not contention.

### @LETTER@2. The Linux adapter in docker

1. On a machine with docker, run `go test -count=1 -tags integration -run TestLinuxAdapterInDocker ./internal/roomspec/`.
2. On a machine with no docker, run the same.

**Expected:** with docker it passes, and the real-fs test is among the tests the container ran. With no docker it skips.
