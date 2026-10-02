# u-joined-click: a joined row answers a click

Status: built, 2026-10-02. Group: bug.

A joined row (`termJoined`, live and not supervised) had an empty click. It now calls `joinedClick(id)` in `js/terminal-list.js`, which asks with `askUser`:

- **message it**: `POST /v1/tasks/{id}/message`, `when: done`. Live.
- **details**: `openTask(id)`. Live.
- **end it**: grey. `/exit` types into a terminal atrium holds and `/kill` stops a runner atrium started; neither reaches a joined session.
- **take it over**: grey. Needs the room to end the session where it runs, then resume it. Filed for the room as `docs/backlog/runtime/u-new-take-over-joined.md`.

`askUser` buttons gained an optional `disabled` reason (drawn off, reason on hover).

**Phone page /m.** It lists cards by status and has no joined state, so a joined card is a normal row that opens its card view, which already has the compose box. Nothing to add there.

Test: `joinedClick`; `joinedLive` now expects the dialog call.
