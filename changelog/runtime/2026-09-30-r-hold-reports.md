- **A launcher tagged `atrium:hold-notices` has its workers' reports held too.** A report goes on the launcher's
  card with `source: report`, rings the board, and does not resume a parked launcher. `atrium_task` with
  `notices: true` reads it back. `atrium:orchestrator` alone still has reports typed or queued. Room side, live at the
  next room restart. The tool text is hub side, live at the next hub deploy. (r-hold-reports)
