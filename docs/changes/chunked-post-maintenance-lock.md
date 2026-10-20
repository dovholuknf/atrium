## Test plan

## @LETTER@. Test git runs no auto maintenance

### @LETTER@1. The shared test config

1. Run `bash scripts/test-touched.sh` after a change to `internal/testguard`.
2. Run `TestAChunkedPostGoesThroughForwarderLinkAndCgi` in `internal/link` with `-tags "integration git"`.

**Expected:** it passes, and `objects/maintenance.lock` is never created in a pushed-to bare repository.
