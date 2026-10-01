# r-new-exit-on-report: a spawned card that reports done exits by itself

Filed by the orchestrator, 2026-10-01.

Status: BUILT 2026-10-01 on `claude/r-exit-on-report` by @runtime. `internal/daemon/exitonreport.go`.

## What happened

pr-tlsuv-378 launched four helper cards through atrium. Each called `atrium_report` with "done" between 15:15 and
15:22 and then sat with its claude process alive until the orchestrator exited them at ~11:35 local. The board
showed them as still running, and clint asked why they were. Nothing in atrium ends a card after its final report:
the worker has to call exit itself, and workers do not.

## The rule

A card with a launcher (`spawned_by_id` set) whose `atrium_report` is a final "done" report is asked to leave
(the same path as `POST /v1/tasks/{id}/exit`) once the report is delivered to the launcher. The card's worktree and
branch are not touched: whether the work is landed is the launcher's call. A report that is not final (a question,
progress, "blocked") changes nothing.

Directors and cards with no launcher are never exited by this. Neither is the orchestrator, nor a card tagged
`atrium:park-idle` or `atrium:hold-notices`, which residents opt in to. A done report carrying an `ask` is a question
and exits nothing. An operator who typed into the terminal within the ceiling quiet period (two minutes) keeps the
card running.

Accepted: a resident an agent launched with none of those tags (a merger, an interviewer, a persona) is exited on its
first done report. No existing tag marks "resident" on its own, so the launcher tags it.

## Acceptance

- A spawned card reporting done is exited within a minute, and its row moves to done with the report kept.
- A report with a question leaves the card running.
- A director's report never exits it.
- The launcher still receives the report before the exit.

## Owner

@runtime.
