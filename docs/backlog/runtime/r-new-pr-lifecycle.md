# r-new-pr-lifecycle. PR lifecycle after the first review

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G3 of docs-deps. Owned by @runtime.

## What is missing

Two things nothing does today:

1. Re-run the recipe when the PR gets new commits.
2. Move a row out of the default view when the PR merges or closes.

Review-tab 1.6 says "when GitHub says it is merged", while intake-design says never poll ticket state. The two
documents disagree and no item settles it.

## Why it is needed

A PR reviewed once stays at its first head, and a merged PR stays on the board as live work.

## Depends on it

- pulls P3 moved-head marks, which only mark and never re-run
- PR and CI state on the card (far-backlog, parked)

## Done looks like

- A decision on how the state arrives (a poll with a bound, a webhook, or on demand when the pulls view opens), and
  the intake-design line amended to match.
- A new head on a reviewed PR offers a re-run, or starts one if the recipe says so, and keeps the earlier findings
  marked against the old head.
- A merged or closed PR leaves the default view and stays reachable by filter.
- Needs the forge interface (`r-new-forge-interface`) for the state query.
