- Every job in the ci workflow now has an absolute `timeout-minutes` (10 for select, rollup and notify, 15 on Linux, 20
  on macOS, 25 on Windows), so a hung runner is killed instead of holding for six hours. (q-ci-job-timeouts)
