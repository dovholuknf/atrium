Docs: none, test tooling only. The design is `docs/rnd/test-layout-design.md`, stage G1.

A Go bucket in `scripts/test-buckets.json` now has `tests`, globs of test files, beside its `sources`. A test file that
no bucket of `internal/api` or `internal/link` owns is in that package's `rest` bucket, which has a `ceiling`.
`node scripts/test-buckets.js --go-run <bucket>` prints the package and the `-run` regex of its tests.
`scripts/ci-report.go` reads the file and groups its report by bucket.

## Test plan

## @LETTER@. Go buckets

### @LETTER@1. The lint passes on the tree

1. Run `node scripts/check-test-buckets.js`.

**Expected:** `check-test-buckets ok`, with the number of Go test files and how many are in `api/rest` and `hub/rest`.

### @LETTER@2. A new test file in no bucket fails

1. Run `node scripts/check-test-buckets.js --probe-go-test internal/api/zzz_integration_test.go`.
2. Run it with `--probe-go-test internal/api/prs_zzz_integration_test.go`.

**Expected:** the first exits 1 and says `api/rest` is over its ceiling, and to put the file in a bucket's `tests`. The
second passes, because the `prs_*test.go` glob owns it.

### @LETTER@3. The -run regex

1. Run `node scripts/test-buckets.js --go-run hub/backlog`.

**Expected:** `./internal/link`, a tab, and `^(TestA|TestB|...)$` of the tests in the bucket's files.

### @LETTER@4. The CI report names the bucket

1. Run `go test -json -tags integration ./internal/hubstore | go run scripts/ci-report.go -dir build.claude/ci`.

**Expected:** after the slowest tests, a `=== buckets` table with `hubstore`, its test count, failures and summed time.
A failed test in `internal/link` prints as `[hub/backlog] <package> <test>`, and `=== failed` lists `hub/backlog: 1 failed`.
