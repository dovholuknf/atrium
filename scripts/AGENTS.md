# scripts/

Everything that is not the Go binary: the test and CI runners, the board checks, release and packaging, room
provisioning, sg4's live start, stop and deploy scripts, and seed data. Read a script's header before running it.
Most take `-WhatIf`, `-Check` or a dry run by default.

## Tests and CI

- **`test.sh` is the one way to run Go tests**, for a person and for CI. `unit` (the default), `all` adds
  `-tags integration`, `full` adds `-tags integration,git`. A file named `*_integration_test.go` starts with
  `//go:build integration` and does not compile without the tag. CI is the only place integration tests run as a
  matter of course.
- **A board unit on `flaky-suite.json` that fails its shard and passes alone on the retry is a PASS, marked and never
  clean.** `test-board-sharded.js` exits 0 and prints `PASSED ON RETRY (slow or stalled, first try failed)` with the
  first-try error and both timings, and the flaky board jobs give their units that one retry. `ci.sh` writes
  `retried.tsv` and a summary section, `ci-rollup.js` and `ci-flake-ledger.js` list it. An unlisted unit that passes
  on retry is `LOAD`, listed the same way, not clean, and passes the run. Go has no rerun, and a unit that fails both tries still fails.
  `test-board-sharded-retry.js` pins it.
- **`flaky-sync.js <run id>` keeps `flaky-suite.json` to the tests that failed in the last 15 main runs.** It records the
  run's failed jobs in `flaky-jobs.jsonl` (from the `failing-tests` artifact `ci-rollup.js` uploads, else the job list and
  logs), adds and removes entries by text edit, and logs each move in `flaky-history.md`. `--dry-run` writes nothing, and
  `test-flaky-sync.js` pins it.
- **A unit test is under 10ms.** `unit-budget.go` prints each test over 10ms and fails the run on one over 20ms. The
  fix is to move the test into an integration file, not to raise the number.
- **Every run is kept** under `build.claude/test-runs/` (the raw `-json` stream and a table, slowest first), so the
  time of a test is never a reason to run the suite again.
- **A manual dispatch can run one os and one suite.** `scripts/ci-select.sh --os all|ubuntu|macos|windows --suite
  all|go|ui` picks the groups, the workflow's `select` job calls it and each job's `if:` and matrix read its outputs.
  Groups named `flaky-board*` or `board*` are ui, every other group is go (`go-checks-visual` included). A push runs
  all. `ci-notify.sh --expected-skipped N` does not count the N jobs the selection skipped as red, any other skip is
  still red. Example: `gh workflow run ci.yml --ref main -f os=windows -f suite=ui`. `test-ci-select.js` pins it.
- **`ci.sh` holds all of CI's logic.** `.github/workflows/ci.yml` only checks out, installs Go and node, and calls it.
  It runs on Linux, macOS and Windows with Git Bash, so nothing in it may assume a tool Git Bash lacks. `--only` runs
  one step and `--list` names them. `--group` runs one workflow job's steps: `go-test-1` and `go-test-2` are the Go
  suite in two halves (daemon cut by test name), `board-1` and `board-2` the gating board units in two halves by
  `board-suite-weights.json`, `go-checks` the rest including the small checks, which run once on the linux job.
  `go-test` and `board` are each whole. The flaky suite is `flaky-go` (Go, no node) and `flaky-board`
  (node and playwright, no Go): the flaky board units and check scripts in one job. `go-checks-visual` and `go-all` merge go-checks, board-visual and (go-all)
  the whole go-test into one job, to use fewer runners. A `board-visual` failure is recorded but never fails the exit
  status. `ci-linux.sh` runs it in docker as a non-root user with no pwsh.
- **CI ends by checking the tree is as it was.** A test that rewrites a tracked file or leaves a new one fails it.
- `test-time-guard.sh` prints `WARN PACKAGE` for a package over 60s and `WARN TEST` for a test over 5s, or over the
  ceiling on its line in `test-time-allow.txt`. It always exits 0: speed is not a functional check. `ci.sh` runs it as
  its own step and puts the WARN lines in `time-guard.log` and `summary.md`.
- `check-test-globals.go` fails on a `_test.go` file assigning a package-level var. Hooks, delays and clocks belong on
  the struct that uses them.
- **`test-touched.sh` is what a worker runs before handing back**: the Go packages, headless units and node tests
  its diff touched, each with the reason it was picked (`test-touched-plan.js` makes the plan,
  `test-touched-selftest.js` checks it). The whole board suite is for after landing: `check-board.sh`,
  `test-board-headless.js` and `test-board-sharded.js` refuse a full run without `FULL_SUITE=1`.
- `check-board.sh` syntax-checks the board, which nothing else parses before a browser does. `check-powershell.ps1`
  checks every `.ps1` parses, Windows job only. The `test-*.ps1` files are tests of scripts and run on Windows only.
- **`merge-check.ps1` is every check a merge needs**, and refuses a branch that adds a `CLAUDE.md`. Those are
  untracked links into dotagents. `new-worktree.ps1` links them into a new worktree, and `setup-merge-worktree.ps1`
  makes the worktree merges happen in, so the main checkout is not locked.

## Rules for the scripts that touch a running atrium

From `scripts/live/README.md`, and they hold for anything here that stops or swaps atrium:

- **A kill is not a stop.** Stop a room with `POST /v1/shutdown` or `atrium stop`.
- **Match a process by its directory and subcommand, never by image name.** `atrium.exe` is also the binary every
  Claude Code hook runs, several times a second.
- **Swap a binary by two renames, never a copy over the live name.**
- **Run stop and start from a shell atrium does not supervise.** A room stopping takes the scripts in its terminals
  with it.
- **The live scripts RUN from a deployed copy**, not from this checkout. After a change lands,
  `install-live-scripts.ps1` copies them over.
- **A hub-only deploy asks `hub-restart-gate.ps1` first**, which waits until no board is in use. Exit 0 is go, 3 and 4
  are do not restart.

## Credentials

**Atrium holds the name of a command and never a credential.** A source (`sources/`) is an argv and an interval, and a
notify command (`notify/`) gets four fields in its environment and stdin, never a token in its argv. Neither may print
a token.

## Release

`cut-release.sh` is a dry run unless `--execute`, and each refusal has its own exit code, which `check-release.sh`
asserts. `release.sh` builds every platform and hashes the results. The `package-*` scripts build the installers from
that.

## The layout

| | |
| --- | --- |
| `test.sh` / `unit-budget.go` / `test-time-guard*.sh` / `ci-report.go` | running the Go suite, and its budgets |
| `ci.sh` / `ci-linux.sh` | everything CI runs, and the same on Linux in docker |
| `check-*.{sh,js,ps1,go}` | checks that no compiler makes: the board, skins, titles, PowerShell, test globals |
| `test-*.js` / `shoot-*.js` / `measure-*.js` / `board-*` | board tests, screenshots, measurements, the sharded suite |
| `merge-check.ps1` / `new-worktree.ps1` / `setup-merge-worktree.ps1` | merging, and worktrees with their `CLAUDE.md` links |
| `install-git-hooks.ps1` | the `post-merge` hook that runs `atrium merged`, never failing a merge |
| `provision-room.ps1` / `room-*.ps1` | making a machine a room over ssh: toolchain, account, folders, gate, spec, start, check |
| `live/` | sg4's own start, stop and deploy scripts. Its README is the rules |
| `release.sh` / `cut-release.sh` / `publish-release.sh` / `package-*` | building and publishing a release |
| `scrub-history.go` / `land-history.go` | rewriting messages and authors before a push to GitHub. Never pushes |
| `fold-changes.ps1` / `backlog-*.ps1` | folding change files into the test plan, and the backlog's file layout |
| `sources/` / `notify/` / `recognisers/` | example sources and notify commands, and the recogniser rows a hub seeds |
| `atrium-service.*` / `atrium-autostart.ps1` / `start-atrium.ps1` | running atrium as a scheduled task or service |
