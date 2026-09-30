# u-new-duplicate-card-row. A card shows twice on the terminals tab, one row live and one stale (bug)

Status: not started. Filed by the orchestrator 2026-09-30 for @ui, from clint's screenshots
(`.atrium/incoming/20260930-074453-pasted.png`, `20260930-074539-pasted.png`). The cause may sit on the hub side
(`internal/link`), so bring in @fabric if it does.

## What clint sees

The orchestrator's card (`claude-sg4~01a06dc7-...`) appears twice, in the pinned block and in its group:

- One row has the room chip (`claude-sg4`), is stuck on an old state (`context 1/3: capture`), and shows `? 1`.
- The other row has no room chip and shows the current state (`context 2/3: clear`). In the first screenshot it also
  had no title.

A reload, or switching to the board tab and back, clears it for a moment. It comes back a few seconds later, every
time. Seen during a new-context cycle started with ctrl-alt-n.

## What the orchestrator found (not a diagnosis)

- `GET /v1/tasks` on the hub (7778) returns ONE row for the card, with id `claude-sg4~01a06dc7-...`.
- The hub's `/v1/events` stream sends `event: task` for the same card with the BARE id `01a06dc7-...`, with no room
  prefix. Captured with `D:\tmp\sse-capture.sh 25 01a06dc7`.
- `internal/link/events.go` `taggedFields` says a `task` event's `id` is tagged with the room, and its comment
  describes exactly this failure: a list that says `sg4~01a0` and a stream that says `01a0` "describes a card the
  board has never heard of". So either the tagging did not run for this event, or the board upserts by the bare id
  and keeps both.
- It started around the 06:50 sg4 room restart (build d7528a87). That build carried r-013 (task events coalesced per
  card, `internal/api/api.go` `PublishTask`/`flushTask`), so r-013 is the first suspect, unproven.

## Seen with it: "new context failed"

The same cycle ended `new context failed` at 07:47, reason `could not type /clear: gave up after 2m0s waiting for an
empty line and no turn in progress`. clint kept talking to the card after the capture step, so a turn was always in
progress when the cycle wanted to type `/clear`. That's r-016's shape (a cycle fails when something is typed between
its steps), and it is likely separate from the duplicate row, but the stuck `1/3: capture` row may be the failed
cycle's last state never cleared. For @runtime: a cycle that finds the card busy should wait for the turn to end, or
say "waiting for this turn to end" on the badge, not give up after 2 minutes.

## Done when

- A live `task` event on the hub always carries the same id the list does, or the board keys rows so that the two
  can never become two rows.
- A headless case: a list row `r~id`, then a `task` event for `id`. One row stays, updated.
