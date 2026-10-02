# u-blocker (board half)

Done. The board reads two new `escalation.source` values from the room, `launch-idle` and `launch-prompt` (with `escalation.prompt`: folder-trust, login, update, other), and treats them as a blocker.

- `isBlocker`, `blockerReason`, `blockerMark` in js/stack.js; `stuckMark` hands a blocker to `blockerMark`, so the stack, board and strip all show it.
- A `blocker` alert in settings-spine.js beside the stuck one, keyed on the count, always on (the "stuck agents" setting only governs the other sources). It rings the permission tone, is pending (retired when the room clears it) and rings again after a reload while still blocked.
- The bell pins one red row per blocked card above the log, with attach, and the bell button is red while any card is blocked. Log lines for a blocker are tinted red.
- Unit `blockerMark` (HEADLESS_ONLY=blockerMark), built on a fixture of the agreed contract; the room side does not exist yet.

Open: the phone page's bell is not changed. The Windows notification list shows the title only, so it carries "BLOCKED" in the title.
