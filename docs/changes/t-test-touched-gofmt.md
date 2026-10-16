## Test plan

## @LETTER@. test-touched checks gofmt

### @LETTER@1. A misformatted file fails

1. In a branch, change a Go file so it is not gofmt clean, for example `now :=time.Now()`.
2. Run `bash scripts/test-touched.sh` (or `pwsh -NoProfile -File scripts/test-touched.ps1`).

**Expected:** a `gofmt -l (changed Go files)` step names the file and the run exits non-zero.

### @LETTER@2. A clean change passes

1. Run `gofmt -w` on the file and run the script again.

**Expected:** the gofmt step is ok. A branch with no changed Go files has no gofmt step.
