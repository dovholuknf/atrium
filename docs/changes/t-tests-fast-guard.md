# t-tests-fast step 4: a guard so the Go suite stays fast

## What it is

`scripts/test-time-guard.sh` reads `go test -json` output, from a file argument or stdin, and fails when a package
takes more than 60s or a single test more than 5s, unless `scripts/test-time-allow.txt` has a line for it. The
parsing is `scripts/ci-report.go`'s, run with a new `-guard-only` flag, so there is one reader of the stream.
`scripts/ci.sh` runs it after the tests on the `go-test.json` it keeps. `scripts/ci-linux.sh` runs `ci.sh` in the
container, so it gets the guard without a change. No workflow YAML changed.

The allowlist line is `<package, Pkg.TestName or Pkg.*> <ceiling in seconds> <reason>`. The ceiling replaces the
default limit for that entry. A subtest answers to its own line, then to its parent's. A failure names the package or
test, its time and the limit, and says to make it faster or add a line with a reason.

## Seeded from today

Measured on the rebased tree with `go test -tags integration -json -p 4 ./...` on a busy Windows box. Packages over
60s: daemon 553s, link 383s, gitsync 254s, store 98s and api 70s. Store and api are 40s and 57 to 74s on a quiet
box, and are listed with room for a busy one. Each ceiling is about 1.5x the time measured. Slow tests are covered by
`Pkg.*` lines for daemon, gitsync, link, deployready and store, and by single lines for the api, hubstore and cli
tests named in `docs/changes/t-tests-fast.md`.

Ceilings are a ratchet: lower a line when you make the thing faster, and delete it when it is no longer needed.

## Tests

Go tests for the allowlist parser and the limits: `go test scripts/ci-report.go scripts/ci-report_test.go`. A bash
self-test over the fixtures in `scripts/testdata/time-guard-*`: `bash scripts/test-time-guard-test.sh`. Both run in
`ci.sh` before the guard.

## Test plan

## @LETTER@. The suite cannot quietly get slow

### @LETTER@1. The guard passes on a real run

1. Run `go test -tags integration -json ./internal/store > store.json`.
2. Run `bash scripts/test-time-guard.sh store.json`.

**Expected:** it prints `ok: no package over 60s ...` and exits 0.

### @LETTER@2. The guard fails on a slow test

1. Run `bash scripts/test-time-guard.sh scripts/testdata/time-guard-slow.json`.

**Expected:** exit 1, with a `SLOW TEST` line naming TestTooSlow, 7.50s and the limit of 5s, and a `SLOW PACKAGE` line
for 61.4s. Each line says to make it faster or add a line to `scripts/test-time-allow.txt` with a reason.

### @LETTER@3. The self-test and parser tests pass

1. Run `bash scripts/test-time-guard-test.sh` and `go test scripts/ci-report.go scripts/ci-report_test.go`.

**Expected:** both pass.