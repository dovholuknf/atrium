# u-new-new-card-late-in-terminals

Incomplete. No measurement and no PNGs: this room has no node, Playwright or go on PATH, so nothing could be run.

## Finding
See the item file. Two causes, both on the path of a new card to the board's list: the room published the card's first
`task` event only after the launch settled, and the board answered a non-whole-row event with the throttled
`tasksSoon` (up to five seconds). The alert and the row share one list, so they were late together. A new card is
`running`, so the strip's filter is not the cause.

## Changed
- `internal/daemon/launch.go`: `publishTask` right after the new card is registered.
- `internal/api/web/js/settings-spine.js`: `onTaskEvent` reads now for an id the card map has not held, once per id.
- `scripts/check-new-card-row.js`: times event to alert to row, fails if the row is over 1s behind the alert. Unrun.
- Item Finding and changelog `changelog/ui/2026-10-04-u-new-new-card-late-in-terminals.md`.

## Tests
None run. To run: `$env:NODE_PATH=<dir with playwright>; node scripts/check-new-card-row.js`, then
`go test ./internal/daemon/...` with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared.

## Still to do
- Run the new script on a machine with Playwright, confirm it fails without the two fixes and passes with them.
- Draw a "starting" chip in place of "joined" for a card with no runner yet, with before/after PNGs under
  `docs/screens/u-new-new-card-late-in-terminals/`.
