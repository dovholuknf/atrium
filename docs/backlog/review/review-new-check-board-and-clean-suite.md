# review-new-check-board-and-clean-suite. check-board.sh red on main, and no clean full suite

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G15 of docs-deps. Owned by @review.

## What is missing

An item for either failure. `check-board.sh` fails three strip-heading asserts on claude/main since d33bd3b5, and it
is noted only in `review/QUEUE.md`. The full daemon suite has no clean run: runtime 0j
`TestGlobalAutoSurvivesAReopen` fails with "database is locked", and runtime 0d `TestIdleParkWorkersFirst` fails 7 of
30.

## Why it is needed

Every @review gate leans on targeted runs while the full run is red, so a regression elsewhere can pass.

## Depends on it

- every @review gate
- the acceptance of the 378 replay

## Done looks like

- The three strip-heading asserts fixed, or the asserts corrected if the heading change was intended.
- 0j and 0d fixed by their runtime items, and one full `go test ./...` plus `check-board.sh` run recorded clean.
- The gate then runs the full suite again and not targeted runs.
