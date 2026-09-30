- **Atrium no longer types its own notices into the orchestrator's terminal.** A launcher tagged
  `atrium:orchestrator` or `atrium:hold-notices` has a worker's silent stop, stuck tool, context size, automatic new
  context and end without a report recorded on its card and on the worker's work item, and the board is sent a
  `notice` event to ring its bell. `atrium_task` with `notices: true` reads them back in one call. A worker's report
  is delivered as before, and every other launcher keeps today's behaviour. Room side, live at the next room restart.
  (r-hold-notices)
