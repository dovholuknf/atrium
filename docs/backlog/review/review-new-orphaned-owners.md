# review-new-orphaned-owners. Re-own the items whose owner no longer exists

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G10 of docs-deps. Owned by @review.

## What is missing

@merge and @terminal own items, and neither runs today. The five directors are fabric, runtime, ui, rnd and review.
Someone must be named owner of each file below, and the file's owner line changed when that happens.

## Files to re-own

Owner @merge:

- `release/77.md` (merge pipeline, the parts not merged)
- `release/m-001.md` (evaluate every test for efficacy)

Owner @terminal:

- `terminal/t-003.md` (the tallest viewer sets the pty height)
- `terminal/74.md` (OpenConsole ConPTY behind a setting)
- `terminal/8.md` (input lag follow-ups, the prefix name)

## Why it is needed

An item with no running owner is never picked up, and a question for it goes nowhere.

## Depends on it

- m-001, 77, 74, t-003 and 8 being worked at all

## Done looks like

- Each file above has a running owner named, chosen by clint or the orchestrator, and the Status line says so.
- `t-003`'s board half stays with @ui, as it already says.
- `README.md` no longer lists a department with no director, or says who answers for `release/` and `terminal/`.
