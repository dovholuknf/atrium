# Agents: writing and landing tests

What QA learned catching red CI on main. Read before you write or land a test. Every rule below cost a CI round.

## Run these before you land (local, seconds)

From the repo root, Go on PATH (`V:\work\tools\go\current\bin`):

1. `go run scripts/check-test-waits.go` : counts flat waits (`waitForTimeout(`, `setTimeout(r,`, `time.Sleep(`) per file against a baseline. A new one fails CI on every OS.
2. `go run scripts/check-test-globals.go` : no package-level vars in tests.
3. `node scripts/check-test-buckets.js` : every test file is in a bucket.
4. `node scripts/flaky-suite.js check` : each flaky-suite entry names a test that exists.
5. Workflow edits: `yq '.jobs | keys | length' .github/workflows/ci.yml` and `bash -n scripts/ci.sh`.

CI runs the same checks first. A job that fails with `FAILED: test waits` (or globals, buckets) and names no test is a check script: read the lines above the FAILED line, they name the file and count.

## Waits

- Wait on a condition under one deadline (`waitForFunction`, `waitForSelector`, poll with a deadline), never a flat sleep.
- A flat wait is allowed only for a negative check (nothing must happen) or a settle with no event to wait on. Mark the line `// wait: <why>` in one line.
- Never widen a timeout to fix a flake. Trace the logs and find the race.
- Landed examples: merge-queue strip tests added two unmarked `waitForTimeout(200)` in `scripts/board-tests/merge.js` and a skin-swap `waitForTimeout(60)` in `scripts/test-board-headless.js`. Three jobs went red (ubuntu go-all, macOS go-test, windows build-board-1) on a check, not a test. Fix was the three `// wait:` marks.

## Flaky tests

- The flaky suite (`scripts/flaky-suite.json`) is empty by design. `node scripts/flaky-sync.js <run>` adds a test that failed in CI; a graduated test is not re-added (floor in the store).
- Per CI run on main: `node scripts/ci-flake-ledger.js ingest --conclusion success|failure <run>`, then flaky-sync, then `flaky-suite.js check`, then commit `scripts/flaky-jobs.jsonl` as `Flaky job store: run <id>`. `ingest` for a red run needs the failing-tests artifact downloaded first; without it flaky-sync still records the failed jobs.
- No product code to fix a flaky test. Small test-side changes only.
- More than 2 failing tests in one run, or 3 red runs in a row, is a real problem: report it.

## Board (playwright) tests

- Board proof needs playwright. A machine without it SKIPS headless units silently. A real run prints a TIMING line. `ssh claudevm` has playwright.
- Load, repeat and stress runs go on the sg box only, never a burner and never the full suite locally.
- Tests that measure layout must wait for animations (tray slide-in) to end before measuring.

## Landing

- The author's tests are the author's to keep green: run the checks above before asking to land. Cherry-picks keep the author's commit; a fix commit is one short line, no trailer.
- Strip `BRIEF.md` from any worker commit before it reaches main.
- Read the CI failure with `gh run view <run> --repo dovholuknf/atrium --log-failed` and grep `FAILED:`, `FAIL`, `panic`.
- A deterministic failure: reproduce the single test locally and fix in one pass, not a CI round per guess.

## Reporting

- Say PROVEN only for a local repro that failed before and passed after. CI is the real test.
- Say plainly whether anything was fixed. Never state a number you did not compute.
