## Test plan

## @LETTER@. The time guard warns and never fails

### @LETTER@1. A slow package and a slow test warn

1. Run `bash scripts/test-time-guard.sh scripts/testdata/time-guard-slow.json` with
   `ALLOW=scripts/testdata/time-guard-allow.txt`.
2. Check the exit status with `echo $?`.

**Expected:** two `WARN TEST` lines and one `WARN PACKAGE` line, the summary says `0 failures, 3 warnings`, and the
exit status is 0. No line says `SLOW`.

### @LETTER@2. The self-test covers each case

1. Run `bash scripts/test-time-guard-test.sh`.

**Expected:** `ok: test-time-guard self-test`. It checks a package-only, a test-only and a both fixture, and that a
missing input file still exits 2.

### @LETTER@3. The job stays green and the warnings are in the summary

1. Run `bash scripts/ci.sh --group go-test` on a run where a package is over its limit.

**Expected:** the `time-guard` step is `ok` in `steps.tsv`, `time-guard.log` holds the WARN lines, and `summary.md` has a
`Time warnings` section. The job exits 0 unless a test failed.
