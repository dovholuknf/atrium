# u-new-new-card-late-in-terminals. A new card is announced long before the terminals list shows it

Status: not started. Owned by @ui (with @runtime if the row is late from the room). Filed by the orchestrator
2026-10-01, from clint.

Screenshot of the row once it showed: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-103146-pasted.png`
(pr-tlsuv-378, claude-sg4).

## What happened

clint got a notification that the card pr-tlsuv-378 was on the board. "A LONG time later" it showed up in the
terminals list.

## Find out

- Where the gap is: the notification fires off the task event, the strip draws from `cardRows` filtered to
  supervised, pinned or joined (`terminal-list.js`). Suspects: the first event is not a whole row, so the list waits
  on the throttled re-read (`tasksSoon`, TASKS_EVERY 5s, trailing, pushed back by every pass); or the card is not
  `supervised` until its runner is up, so the strip filters it out until the next re-read after that.
- Measure on a headless board with a mocked launch: time from the alert to the row.

## Wanted

- The row appears in the terminals list at the same moment as the alert, even before its runner is up (drawn as
  starting).
