Docs: none, test dependency injection leaves checkout behavior unchanged.

## Test plan

## @LETTER@. Origin slug validation without disk access

### @LETTER@1. Run touched tests

1. Run `bash scripts/test-touched.sh`.

**Expected:** API tests pass, including forge URL acceptance and credential and local path refusal with an
in-memory config reader. The origin slug unit test creates no temporary directory and performs no disk I/O.
