## Test plan

## @LETTER@. Headless wait timing

### @LETTER@1. A hung page load is diagnosed

1. Run `node scripts/test-timeout-diag.js`.
2. It prints `timeout-diag: ok`, which covers a goto that never answers: one TIMEOUT-DIAG line with the URL and
   `waitedMs`, no `diagError`, and a SLOW-WAIT line.

### @LETTER@2. Wait times are logged

1. Run a few headless units with `HEADLESS_UNITS=bootClean node scripts/test-board-headless.js`.
2. The output ends with one `WAIT-TIMES` JSON line with count, p50, p95, max and the 5 slowest per kind.
