# Flaky suite as two jobs per platform

## @LETTER@. The flaky suite runs as two jobs per platform

**Do:** read the `flaky-*` matrices in `.github/workflows/ci.yml`, then run `bash scripts/ci.sh --list-groups`.

**Expected:** each matrix lists `flaky-go` and `flaky-board`, so CI starts six flaky jobs. `flaky-go` sets up Go and no
node, `flaky-board` sets up node and no Go, and `flaky-board` runs every flaky board unit and flaky check script.
