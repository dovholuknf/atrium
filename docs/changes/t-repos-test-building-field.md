## Test plan

## @LETTER@. The repos list answers a snapshot object

### @LETTER@1. Empty and seeded store

1. Run `go test -tags integration ./internal/link/ -run TestReposIsOpenLikeGrowlsAndExactlyShaped -count=1`.

**Expected:** it passes. An empty store answers `repos` as `[]` with a `building` field, from off loopback too, and a
seeded store lists its repositories once the snapshot has finished building.
