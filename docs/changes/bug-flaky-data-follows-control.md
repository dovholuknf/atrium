## Test plan

## @LETTER@. The data-follows-control test is stable

### @LETTER@1. Repeat the test

1. Run `go test -tags integration -count=30 -run TestDataFollowsTheControl ./internal/link` twice, then the same with `-race -count=10`.

**Expected:** every run passes. Before the fix about 4 in 30 failed with "the room dialled the hub's stale advertise 1 times", because the room's path watcher probes every rung it is not standing on, the stale advertise included, and the test counted that probe as a data dial.
