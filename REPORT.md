# u-new-details-debug-section

## What changed

Most of the item was already on this branch from e310a401 (2026-10-01): the debug section in the terminal drawer,
the countdown, outside click and Escape, and the switches moved in. This pass finished the rest.

- `js/peek-debug.js`: the debug rows now reuse `typingGateText(s, held)`. When a message is held and blocked, a
  "blocked" row says it in the gate line's own words.
- `scripts/test-board-headless.js` (`termDebug`):
  - new checks: a click in the terminal closes the drawer and focuses the terminal, and the card popover carries no
    debug section
  - the gate line expectations were stale against `typingGateText` and now match the design note
  - the input lag step waits for an in-flight `/v1/settings` read before changing the mock. This failed on the
    branch before my changes too, because the read is shared while in flight
  - `DEBUG_SHOTS=<dir>` (and `DEBUG_SHOTS_TAG`) writes the drawer PNGs
- `changelog/ui/2026-10-04-u-new-details-debug-section.md`, backlog item marked done with its design note.

## Design notes

- The settings dialog points at the drawer and no longer has the two checkboxes. One place to look.
- The switches read and write the same localStorage keys as before, so the cross-window work needs nothing from here.
- The gate is read only while the drawer is open, and not in a hidden tab.

## Tests

`NODE_PATH=<a sibling worktree's node_modules> HEADLESS_ONLY=termDebug node scripts/test-board-headless.js`
with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared: `termDebug ok`. Only that unit was run, not the full
suite. This worktree has no node_modules, so playwright came from `u-new-context-bar-on-rows`.

## PNGs

`docs/screens/u-new-details-debug-section/`: `before-` and `after-` each for `gate-closed-countdown.png` and
`gate-open.png`. Real board code, headless. Before is without the "blocked" row, after has it.
