## Test plan

## @LETTER@. CI is green on both platforms, and a red run explains itself

`scripts/ci.sh` is what `.github/workflows/ci.yml` runs on ubuntu-latest and windows-latest. It runs the same way on a
laptop: four packages at a time with GOMAXPROCS=4 like a runner (`CI_CPUS` changes it), `-count=1` so a cached pass is
never taken for a run, and every `ATRIUM_*` variable cleared. `scripts/ci-linux.sh` runs it on Linux in docker, as the
non-root uid a runner uses, with node and pwsh like the runner and a fresh copy of the tree.

### @LETTER@1. Windows, three runs in a row

1. In Git Bash at the repository root, `bash scripts/ci.sh` three times.

**Expected:** each ends `everything passed.` The first step, `the machine`, prints the OS, CPUs, GOMAXPROCS, go,
temp dirs, git, node, pwsh and which `ATRIUM_*` variables were cleared.

### @LETTER@2. Linux in docker, three runs in a row

1. With docker reachable (on sg4 `DOCKER_HOST=tcp://127.0.0.1:2375`), `bash scripts/ci-linux.sh 3`.

**Expected:** `linux run 3 of 3 passed`. The first use builds `atrium-ci-linux:go<version>` from go.mod's Go
version. The files of each run are copied back to `build.claude/ci-linux/<when>-<n>/`.

### @LETTER@3. One test, repeated, on Linux

1. `bash scripts/ci-linux.sh -- go test -count=20 -run TestGuardHookOverride ./internal/cli/`

**Expected:** `ok` from inside the container. This is the quick way to chase a test that fails only on ubuntu.

### @LETTER@4. A failing test explains itself from the log

1. Add `t.Fatal("x")` to any test in `internal/daemon` that starts a daemon (TestShutdownIsPrompt, say).
2. `bash scripts/ci.sh`, then undo the edit.

**Expected:** under `go test`, a `--- FAIL:` block with only that test's output, a `testdiag:` line naming the dump
file under `build.claude/ci/goroutines/` and listing the goroutines inside the store or database/sql, and at the end
`=== failed` naming the test. `build.claude/ci/` holds `go-test.json`, the failed package's whole output, and
`timings.tsv`. On GitHub the same folder is the `ci-<os>-attempt-<n>` artifact of the run.

### @LETTER@5. A hang names the test that hung

1. Add `time.Sleep(time.Hour)` to a test in `internal/cli`, and in `scripts/ci.sh` change `-timeout 20m` to
   `-timeout 30s`.
2. `bash scripts/ci.sh`, then undo both edits.

**Expected:** `internal/cli failed with no failing test`, then `still running when ... ended:` with the test's name
and the timeout's goroutine dump.

### @LETTER@6. What was red

1. `go test -count=1 -run TestGuardHookOverride ./internal/cli/` on Windows and in docker.
2. `go test -count=1 -run "TestLastOutput|TestAStartupFailureWhileNoDaemon" ./internal/daemon/` on both.
3. `gofmt -l .`

**Expected:** `ok` everywhere, and gofmt prints nothing under `internal/`.
