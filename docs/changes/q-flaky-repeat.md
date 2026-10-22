## Test plan

## @LETTER@. Each flaky group runs five times in a row

### @LETTER@1. The count is one variable

1. Run `bash scripts/ci.sh --group flaky-go` with `FLAKY_REPEAT` unset, then with `FLAKY_REPEAT=2`.
2. Read `env:` at the top of `.github/workflows/ci.yml`.

**Expected:** unset runs one pass with no header. `FLAKY_REPEAT=2` prints `--- flaky pass 1 of 2` and `--- flaky pass 2
of 2`, and `go-flaky/` and `go-flaky-p2/` exist. The workflow sets `FLAKY_REPEAT: 5` once.

### @LETTER@2. A test failing in some passes is one failing test

1. Run `node scripts/test-ci-rollup.js`.
2. In a CI run where a flaky test fails, read the run summary and the `failing-tests` artifact.

**Expected:** the test is listed once as `(failed N of 5 passes)`, every pass ran, and the job is red.
