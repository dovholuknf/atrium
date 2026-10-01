- **A launched worker that reports `done` is asked to leave.** After the final `atrium_report` is recorded and queued to
  the launcher, a card with `spawned_by_id` set is asked to exit five seconds later, by the same path as
  `POST /v1/tasks/{id}/exit`. The card stays `done` with its report. `question`, `progress` and `blocked` reports,
  cards with no launcher, `atrium:director` and `atrium:orchestrator` cards are never exited, and no worktree or
  branch is touched. A failed exit is logged and the report still stands. Item r-new-exit-on-report.
