- **CI fails when the Go suite gets slow.** A package over 60s or a single test over 5s fails `scripts/ci.sh` (and so
  `ci-linux.sh`) unless `scripts/test-time-allow.txt` has a line for it with a ceiling and a reason. (t-tests-fast)
