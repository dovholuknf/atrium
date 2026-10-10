# REPORT: t-unit-vs-integration

Every test is tagged by clint's final rule (relayed by the orchestrator, replacing the brief's): a unit test runs in
under 10ms, touches no store, file, socket, httptest server or process, and never sleeps. Everything else is in a
`*_integration_test.go` file under `//go:build integration`. 175 of 4,045 tests stay unit.

## Numbers

- `go test -list '.*' ./...` of hub/claude/main before, and `go test -tags integration -list '.*' ./...` after: 4,045
  lines each, **identical per package and name** (diff empty). Nothing deleted, renamed or weakened.
- Unit only: 175 listed, 165 top-level tests ran (the rest are Example/Fuzz-style entries that `-json` does not report
  as tests). All pass, three runs, none at or over 10ms.
- Before: `go test -count=1 ./...` 188s wall (3:07.8), 4,035 top-level tests in `-json` (3,995 pass, 37 skip, 3 fail).
- After: `bash scripts/test.sh` about 6.2s wall, 165 tests, 0 failures, 0 over budget.
- Integration tests were not run, per the orchestrator's rule. I proved the count by `-list` instead. The three
  failures in the baseline (ptyhost TestStartRunsADetachedHostFromACopy and TestHostCloseKillsItsRunners, daemon
  TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP) are integration tests now, so CI will show them. They were
  already failing before my change.
- `go build -o build.claude/ ./...`, `go vet ./...`, `go vet -tags integration ./...` pass, and so do both vets under
  GOOS=windows and GOOS=linux.

## Per package

Package time is go test's wall for the package; after includes about a second of binary start and link.

| Package | Tests | Unit | Integration | Package time before (s) | Unit time after (s) |
| --- | ---: | ---: | ---: | ---: | ---: |
| internal/daemon | 1655 | 44 | 1611 | 172.0 | 2.90 |
| internal/link | 708 | 3 | 705 | 170.5 | 1.27 |
| internal/gitsync | 231 | 1 | 230 | 137.5 | 1.55 |
| internal/api | 301 | 0 | 301 | 42.4 | 3.17 |
| internal/store | 438 | 1 | 437 | 25.0 | 1.21 |
| internal/deployready | 34 | 1 | 33 | 20.9 | 2.98 |
| internal/guard | 9 | 8 | 1 | 8.0 | 1.54 |
| internal/ptyhost | 20 | 2 | 18 | 7.2 | 1.27 |
| internal/cli | 205 | 8 | 197 | 6.6 | 2.57 |
| internal/hubstore | 154 | 0 | 154 | 5.4 | 0.00 |
| internal/runnersetup | 36 | 3 | 33 | 4.7 | 1.39 |
| internal/roomstats | 21 | 4 | 17 | 2.1 | 1.32 |
| internal/edge | 10 | 10 | 0 | 1.3 | 3.08 |
| internal/claudeconf | 73 | 1 | 72 | 1.1 | 3.39 |
| internal/forge | 30 | 17 | 13 | 0.9 | 1.50 |
| internal/cardproc | 2 | 0 | 2 | 0.7 | 0.00 |
| internal/cardcolors | 2 | 2 | 0 | 0.5 | 3.17 |
| internal/roomspec | 41 | 33 | 8 | 0.5 | 1.30 |
| internal/cardurl | 1 | 1 | 0 | 0.4 | 3.31 |
| internal/inputlag | 4 | 4 | 0 | 0.3 | 1.53 |
| internal/safepath | 14 | 3 | 11 | 0.2 | 1.16 |
| internal/linkfetch | 2 | 0 | 2 | 0.2 | 0.00 |
| scripts/recognisers | 2 | 2 | 0 | 0.2 | 1.06 |
| internal/runnerprofile | 3 | 3 | 0 | 0.2 | 1.35 |
| internal/prreview/render | 21 | 1 | 20 | 0.2 | 1.27 |
| internal/webasset | 6 | 6 | 0 | 0.2 | 1.23 |
| internal/requirements | 7 | 6 | 1 | 0.2 | 1.26 |
| internal/testdiag | 1 | 0 | 1 | 0.2 | 0.00 |
| internal/itemgate | 5 | 5 | 0 | 0.2 | 1.27 |
| internal/resources | 3 | 0 | 3 | 0.2 | 0.00 |
| internal/shellpick | 6 | 6 | 0 | 0.2 | 1.19 |
| **total** | 4045 | 175 | 3870 | | |

## How the tests were found

Not by reading 4,000 tests. Three signals, a test is integration if any fires:

1. **Measured effects.** A copy of the Go toolchain, patched to log every `forkExec`, `Mkdir`, `socket` and every
   `open` that creates a file or lives under a temp dir, ran each package serially (`-p 1 -parallel 1 -json`).
   Each log line was matched to the test whose run window held it. 3,880 of 4,045 tests do one of these.
2. **Static.** An AST tool (`go/parser`, not committed) flagged tests, and the test helpers they call, that name
   `exec.Command`, `pty`/`ptyhost`, `t.TempDir`, `os` file calls, `httptest.NewServer`, `net.Listen/Dial`,
   `time.Sleep/After/NewTimer/NewTicker/Tick/AfterFunc` or `sql.Open`. This catches what this machine's run could not
   see, such as a Windows-only test.
3. **Time.** Any test at 10ms or more in either the loaded baseline or the serial run.

The tool then moved each such test, and every helper, type, var and const only they use, into
`<file>_integration_test.go`, pruned imports in both files, and deleted a `_test.go` file left with no declarations
(597 files). A type that stays unit keeps all its methods.

## Judgment calls

- **The brief's rule was replaced mid-job** (no stores, no temp files, no sockets, under 10ms, no sleeps). By it nearly
  everything is integration, so `internal/store`, `hubstore`, `api`, `claudeconf` and the like are integration even
  though they are in-process; the brief's "slow unit test stays unit" exception no longer applies. Tests that need
  `t.TempDir` or an sqlite store are integration whatever their speed.
- **Unsure meant integration.** Test time resolution is 10ms, so 0.01s in either run counted as slow.
- **Unit tests that remain**: pure logic such as parsers, formatters, rules, and the screen/ring buffer code in
  daemon (44), roomspec (33), forge (17), edge (10), guard (8), cli (8).
- **OS files.** `X_windows_integration_test.go` is no longer a Windows-only file by name, so each such file carries
  `//go:build integration && windows` (or linux, darwin, `!windows` for the old `_unix` one) explicitly.
- **Scripts.** ci.sh, the Makefile and ci.yml are untouched (the orchestrator did them). I added the 10ms budget to
  `scripts/test.sh` (unit mode pipes `go test -json` into `scripts/unit-budget.go`; `-json` by hand opts out) as asked.
  The budget uses the run-to-end timestamp gap for ms precision, since go test's `Elapsed` has two decimals.
- **AGENTS.md** does not exist in this repository, and the orchestrator said to ignore that request. The rule is in
  the build tag, the `scripts/test.sh` header and `docs/changes/t-unit-vs-integration.md`.
- **Not run:** any integration test, anywhere. Whether they still pass in the tagged files is for CI to say. Vet and
  `-list` show they compile and are the same set.
