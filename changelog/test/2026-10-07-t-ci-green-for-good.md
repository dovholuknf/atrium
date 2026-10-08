CI is green on ubuntu and windows again: a gofmt slip, a guard test that only knew Windows paths, and a failure line
that kept a terminal escape (`ESC =`) from pwsh. A red run now explains itself: one line per package, only the failed
tests' output, goroutine dumps from failing daemon tests, and the raw run uploaded as an artifact. `scripts/ci.sh`
runs locally as on a runner, and `scripts/ci-linux.sh` runs it on Linux in docker. Item t-ci-green-for-good.
